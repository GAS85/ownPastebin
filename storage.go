package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/net/context"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// PasteData is the JSON payload stored in all backends.
type PasteData struct {
	Content      string `json:"content"`       // base64 or AES-GCM ciphertext
	Burn         bool   `json:"burn"`
	Encrypted    bool   `json:"encrypted"`     // server-side encryption applied
	E2EEncrypted bool   `json:"e2e_encrypted"` // client-side encryption
	Lang         string `json:"lang"`
}

// Storage is the common interface all backends implement.
type Storage interface {
	Save(key string, data *PasteData, ttl time.Duration) error
	Get(key string) (*PasteData, error)
	Delete(key string) error
	// GetAndDelete atomically reads and removes — used for burn-on-read.
	GetAndDelete(key string) (*PasteData, error)
	Close() error
}

// ---- helpers ----------------------------------------------------------------

func marshalPaste(d *PasteData) ([]byte, error) {
	return json.Marshal(d)
}

func unmarshalPaste(b []byte) (*PasteData, error) {
	var d PasteData
	return &d, json.Unmarshal(b, &d)
}

// =============================================================================
// SQLite
// =============================================================================

type SQLiteStorage struct {
	db   *sql.DB
	mu   sync.Mutex
}

func newSQLiteStorage(path string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer; WAL handles concurrent readers

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS pastes (
		id        TEXT PRIMARY KEY,
		data      TEXT NOT NULL,
		expire_at INTEGER
	)`)
	if err != nil {
		return nil, err
	}

	s := &SQLiteStorage{db: db}
	go s.cleanupLoop()
	return s, nil
}

func (s *SQLiteStorage) Save(key string, d *PasteData, ttl time.Duration) error {
	b, err := marshalPaste(d)
	if err != nil {
		return err
	}
	var expireAt *int64
	if ttl > 0 {
		t := time.Now().Add(ttl).Unix()
		expireAt = &t
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO pastes (id, data, expire_at) VALUES (?, ?, ?)`,
		key, string(b), expireAt,
	)
	return err
}

func (s *SQLiteStorage) Get(key string) (*PasteData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT data, expire_at FROM pastes WHERE id = ?`, key)
	var raw string
	var expireAt *int64
	if err := row.Scan(&raw, &expireAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if expireAt != nil && time.Now().Unix() >= *expireAt {
		s.db.Exec(`DELETE FROM pastes WHERE id = ?`, key) //nolint
		return nil, nil
	}

	return unmarshalPaste([]byte(raw))
}

func (s *SQLiteStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM pastes WHERE id = ?`, key)
	return err
}

// GetAndDelete uses RETURNING for atomicity (SQLite ≥ 3.35, 2021).
func (s *SQLiteStorage) GetAndDelete(key string) (*PasteData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`
		DELETE FROM pastes
		WHERE id = ?
		  AND (expire_at IS NULL OR expire_at > ?)
		RETURNING data`, key, time.Now().Unix())

	var raw string
	if err := row.Scan(&raw); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return unmarshalPaste([]byte(raw))
}

func (s *SQLiteStorage) Close() error { return s.db.Close() }

func (s *SQLiteStorage) cleanupLoop() {
	for {
		time.Sleep(time.Hour)
		s.mu.Lock()
		s.db.Exec(`DELETE FROM pastes WHERE expire_at IS NOT NULL AND expire_at < ?`, time.Now().Unix()) //nolint
		s.mu.Unlock()
	}
}

// =============================================================================
// PostgreSQL
// =============================================================================

type PostgresStorage struct {
	db *sql.DB
}

func newPostgresStorage(dsn string) (*PostgresStorage, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS pastes (
		id        TEXT PRIMARY KEY,
		data      JSONB NOT NULL,
		expire_at TIMESTAMPTZ
	)`)
	if err != nil {
		return nil, err
	}

	return &PostgresStorage{db: db}, nil
}

func (s *PostgresStorage) Save(key string, d *PasteData, ttl time.Duration) error {
	b, err := marshalPaste(d)
	if err != nil {
		return err
	}
	var expireAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expireAt = &t
	}
	_, err = s.db.Exec(`
		INSERT INTO pastes (id, data, expire_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, expire_at = EXCLUDED.expire_at`,
		key, string(b), expireAt,
	)
	return err
}

func (s *PostgresStorage) Get(key string) (*PasteData, error) {
	row := s.db.QueryRow(`
		SELECT data FROM pastes
		WHERE id = $1 AND (expire_at IS NULL OR expire_at > NOW())`, key)

	var raw string
	if err := row.Scan(&raw); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return unmarshalPaste([]byte(raw))
}

func (s *PostgresStorage) Delete(key string) error {
	_, err := s.db.Exec(`DELETE FROM pastes WHERE id = $1`, key)
	return err
}

// GetAndDelete is fully atomic via DELETE ... RETURNING.
func (s *PostgresStorage) GetAndDelete(key string) (*PasteData, error) {
	row := s.db.QueryRow(`
		DELETE FROM pastes
		WHERE id = $1 AND (expire_at IS NULL OR expire_at > NOW())
		RETURNING data`, key)

	var raw string
	if err := row.Scan(&raw); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return unmarshalPaste([]byte(raw))
}

func (s *PostgresStorage) Close() error { return s.db.Close() }

// =============================================================================
// Redis
// =============================================================================

type RedisStorage struct {
	client *redis.Client
}

func newRedisStorage(url string) (*RedisStorage, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return &RedisStorage{client: client}, nil
}

func (s *RedisStorage) Save(key string, d *PasteData, ttl time.Duration) error {
	b, err := marshalPaste(d)
	if err != nil {
		return err
	}
	ctx := context.Background()
	return s.client.Set(ctx, key, b, ttl).Err()
}

func (s *RedisStorage) Get(key string) (*PasteData, error) {
	ctx := context.Background()
	b, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return unmarshalPaste(b)
}

func (s *RedisStorage) Delete(key string) error {
	return s.client.Del(context.Background(), key).Err()
}

// GetAndDelete uses GETDEL — atomic in Redis.
func (s *RedisStorage) GetAndDelete(key string) (*PasteData, error) {
	ctx := context.Background()
	b, err := s.client.GetDel(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return unmarshalPaste(b)
}

func (s *RedisStorage) Close() error { return s.client.Close() }

// =============================================================================
// Backend selector
// =============================================================================

func newStorage(cfg *Settings) Storage {
	if cfg.RedisURL != "" {
		s, err := newRedisStorage(cfg.RedisURL)
		if err == nil {
			log.Println("[storage] using Redis")
			return s
		}
		log.Printf("[storage] Redis unavailable (%v), falling back", err)
	}

	if cfg.PostgresURL != "" {
		s, err := newPostgresStorage(cfg.PostgresURL)
		if err == nil {
			log.Println("[storage] using PostgreSQL")
			return s
		}
		log.Printf("[storage] Postgres unavailable (%v), falling back", err)
	}

	s, err := newSQLiteStorage(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("[storage] SQLite failed: %v", err)
	}
	log.Printf("[storage] using SQLite at %s", cfg.SQLitePath)
	return s
}
