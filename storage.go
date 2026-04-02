package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/net/context"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// PasteData is the JSON payload stored in all backends.
type PasteData struct {
	Content      string     `json:"content"`             // base64 or AES-GCM ciphertext
	Burn         bool       `json:"burn"`
	Encrypted    bool       `json:"encrypted"`           // server-side encryption applied
	E2EEncrypted bool       `json:"e2e_encrypted"`       // client-side encryption
	Lang         string     `json:"lang"`
	ExpireAt     *time.Time `json:"expire_at,omitempty"` // nil = never expires
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

// burnScript atomically reads PTTL then GETDEL in a single round-trip.
// PTTL must come BEFORE GETDEL because GETDEL removes the key and its TTL.
// Lua false (Redis nil bulk reply) is returned as Go nil via the Slice() call,
// so the res[0].(string) type assertion correctly falls through to the nil guard.
var burnScript = redis.NewScript(`
	local ttl = redis.call('PTTL', KEYS[1])
	local val = redis.call('GETDEL', KEYS[1])
	if not val then
		return {false, 0}
	end
	return {val, ttl}
`)

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
	db     *sql.DB
	dbPath string
	stop   chan struct{}
	wg     sync.WaitGroup
}

func newSQLiteStorage(path string, cfg *Settings) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	// WAL mode supports 1 writer + N concurrent readers without a Go-level mutex.
	// _busy_timeout in the DSN makes writers retry on lock contention automatically.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	// page_size must be set on a NEW database before the first table is created.
	// On an existing DB it is silently ignored.
	if err := applyPageSize(db, cfg.SQLitePageSize); err != nil {
		db.Close()
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS pastes (
		id        TEXT PRIMARY KEY,
		data      TEXT NOT NULL,
		expire_at INTEGER
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	if err := ensureIncrementalVacuum(db); err != nil {
		db.Close()
		return nil, err
	}

	s := &SQLiteStorage{
		db:     db,
		dbPath: path,
		stop:   make(chan struct{}),
	}
	s.wg.Add(1)
	go s.cleanupLoop()
	return s, nil
}

func applyPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA synchronous=NORMAL",
		"PRAGMA temp_store=MEMORY",       // temp tables in RAM
		"PRAGMA mmap_size=268435456",     // 256 MB memory-mapped I/O
		"PRAGMA foreign_keys=ON",
		"PRAGMA auto_vacuum=INCREMENTAL", // space reclamation (ensureIncrementalVacuum activates it)
		"PRAGMA cache_size=-20000",       // ~20 MB page cache (negative = KiB)
		"PRAGMA wal_autocheckpoint=1000", // let SQLite auto-manage WAL checkpoints
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("failed to apply %s: %w", p, err)
		}
	}
	return nil
}

// applyPageSize sets PRAGMA page_size on a new (empty) database.
// On an existing database SQLite silently ignores this.
// Changing page_size on a populated DB requires a full VACUUM and will be executed
// with next VACUUM maximum in 50 hours.
//
// Valid values: 512, 1024, 2048, 4096 (default), 8192, 16384, 32768, 65536.
//   - 4096 (default) — good for typical text pastes (< 100 KB)
//   - 8192 or 16384  — better when pastes are regularly several MB, because
//     each paste fits in fewer pages, reducing I/O and B-tree depth
//
// 0 or unset → SQLite default (4096). Invalid values are ignored with a warning.
func applyPageSize(db *sql.DB, size int) error {
	valid := map[int]bool{512: true, 1024: true, 2048: true, 4096: true,
		8192: true, 16384: true, 32768: true, 65536: true}

	if size == 0 {
		return nil // use SQLite default (4096)
	}
	if !valid[size] {
		slog.Warn("PASTEBIN_SQLITE_PAGE_SIZE is not a valid power-of-2 between 512 and 65536; using SQLite default",
			"requested", size)
		return nil
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA page_size=%d`, size)); err != nil {
		return fmt.Errorf("set page_size=%d: %w", size, err)
	}
	slog.Debug("SQLite page_size set", "size", size,
		"note", "only effective on a new empty database")
	return nil
}

// ensureIncrementalVacuum checks the current auto_vacuum mode and migrates the
// DB if needed. Migration requires a full VACUUM to rewrite the file header —
// this is a one-time cost logged clearly so operators know what happened.
//
// auto_vacuum modes: 0 = none (default), 1 = full, 2 = incremental.
func ensureIncrementalVacuum(db *sql.DB) error {
	var mode int
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return fmt.Errorf("read auto_vacuum mode: %w", err)
	}
	if mode == 2 {
		slog.Debug("SQLite auto_vacuum already incremental, no migration needed")
		return nil
	}

	slog.Info("SQLite auto_vacuum migration starting", "current_mode", mode, "target_mode", 2)

	// applyPragmas already sets auto_vacuum=INCREMENTAL; the VACUUM below
	// is what actually rewrites the file header to activate the new mode.
	slog.Info("running one-time VACUUM to activate incremental auto_vacuum (may take a moment on large DBs)")
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("migration VACUUM failed: %w", err)
	}

	// Verify the mode actually changed — VACUUM on a WAL-mode DB with active
	// readers will silently fail to change the header.
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return fmt.Errorf("verify auto_vacuum after migration: %w", err)
	}
	if mode != 2 {
		// Not fatal — incremental_vacuum calls will be no-ops until next restart.
		slog.Warn("auto_vacuum mode did not change after VACUUM — another process may have the DB open; will retry on next startup",
			"mode", mode,
		)
		return nil
	}

	slog.Info("SQLite auto_vacuum migration complete", "mode", mode)
	return nil
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
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO pastes (id, data, expire_at) VALUES (?, ?, ?)`,
		key, string(b), expireAt,
	)
	return err
}

func (s *SQLiteStorage) Get(key string) (*PasteData, error) {

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

	paste, err := unmarshalPaste([]byte(raw))
	if err != nil {
		return nil, err
	}
	if expireAt != nil {
		t := time.Unix(*expireAt, 0)
		paste.ExpireAt = &t
	}
	return paste, nil
}

func (s *SQLiteStorage) Delete(key string) error {
	_, err := s.db.Exec(`DELETE FROM pastes WHERE id = ?`, key)
	return err
}

// GetAndDelete uses RETURNING for atomicity (SQLite ≥ 3.35, 2021).
func (s *SQLiteStorage) GetAndDelete(key string) (*PasteData, error) {

	row := s.db.QueryRow(`
		DELETE FROM pastes
		WHERE id = ?
		  AND (expire_at IS NULL OR expire_at > ?)
		RETURNING data, expire_at`, key, time.Now().Unix())

	var raw string
	var expireAt *int64
	if err := row.Scan(&raw, &expireAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	paste, err := unmarshalPaste([]byte(raw))
	if err != nil {
		return nil, err
	}
	if expireAt != nil {
		t := time.Unix(*expireAt, 0)
		paste.ExpireAt = &t
	}
	return paste, nil
}

func (s *SQLiteStorage) Close() error {
	close(s.stop)
	s.wg.Wait()
	// The goroutine is fully stopped.

	var freelistBefore, freelistAfter int
	s.db.QueryRow(`PRAGMA freelist_count`).Scan(&freelistBefore)
	if freelistBefore > 0 {
		if _, err := s.db.Exec(`PRAGMA incremental_vacuum`); err != nil {
			slog.Warn("final incremental vacuum failed", "err", err)
		}
		s.db.QueryRow(`PRAGMA freelist_count`).Scan(&freelistAfter)
		slog.Info("SQLite shutdown vacuum",
			"pages_freed", freelistBefore-freelistAfter,
			"pages_remaining", freelistAfter,
		)
	}

	return s.db.Close()
}

// walFileSize returns the current WAL file size in bytes, or 0 if absent.
func (s *SQLiteStorage) walFileSize() int64 {
	fi, err := os.Stat(s.dbPath + "-wal")
	if err != nil {
		return 0
	}
	return fi.Size()
}

// vacuumDB reclaims SQLite free pages.
// If full=true, runs a full VACUUM (blocks all writers); otherwise runs
// incremental_vacuum up to maxPages pages.
func (s *SQLiteStorage) vacuumDB(full bool, maxPages int) {
	var freelistPages int
		err := s.db.QueryRow(`PRAGMA freelist_count`).Scan(&freelistPages)

	if err != nil {
		slog.Error("vacuumDB: freelist_count failed", "err", err)
		return
	}
	if freelistPages == 0 {
		slog.Debug("vacuumDB: no free pages to reclaim")
		return
	}
	slog.Debug("vacuumDB: free pages available", "count", freelistPages)


	var execErr error
	if full {
		_, execErr = s.db.Exec(`VACUUM`)
	} else {
		_, execErr = s.db.Exec(fmt.Sprintf(`PRAGMA incremental_vacuum(%d)`, maxPages))
	}
	op := "incremental_vacuum"
	if full {
		op = "VACUUM"
	}
	if execErr != nil {
		slog.Error(op+" failed", "err", execErr)
		return
	}

	var remaining int
	s.db.QueryRow(`PRAGMA freelist_count`).Scan(&remaining)
	slog.Debug(op+" completed",
		"pages_reclaimed", freelistPages-remaining,
		"remaining", remaining,
	)
}

func (s *SQLiteStorage) cleanupLoop() {
	defer s.wg.Done()

	// Run hourly and delete expired pastes
	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()

	// Run periodically and reclaim the space
	vacuumTicker := time.NewTicker(6 * time.Hour)
	defer vacuumTicker.Stop()

	// Run periodically and reclaim whole space
	fullVacuumTicker := time.NewTicker(50 * time.Hour)
	defer fullVacuumTicker.Stop()

	// Truncate WAL File
	walCheckTicker := time.NewTicker(20 * time.Minute)
	defer walCheckTicker.Stop()

	// Initial incremental vacuum on startup — reclaim up to 10 000 pages (~40 MB).
	slog.Debug("initial incremental vacuum start")
	_, initErr := s.db.Exec(`PRAGMA incremental_vacuum(10000)`)
	if initErr != nil {
		slog.Error("initial incremental vacuum failed", "err", initErr)
	} else {
		slog.Debug("initial incremental vacuum completed")
	}

	// Integrity check on startup — log any problems found.
	rows, icErr := s.db.Query(`PRAGMA integrity_check`)
	if icErr != nil {
		slog.Error("integrity_check failed", "err", icErr)
	} else {
		defer rows.Close() // always close, even on early exit
		for rows.Next() {
			var msg string
			if err := rows.Scan(&msg); err != nil {
				slog.Error("integrity_check scan failed", "err", err)
				break
			}
			if msg != "ok" {
				slog.Warn("integrity_check", "result", msg)
			}
		}
		if err := rows.Err(); err != nil {
			slog.Error("integrity_check rows error", "err", err)
		}
	}

	for {
		select {
		case <-cleanupTicker.C:
			result, err := s.db.Exec(
				`DELETE FROM pastes WHERE expire_at IS NOT NULL AND expire_at < ?`,
				time.Now().Unix(),
			)
			if err != nil {
				slog.Error("cleanup delete failed", "err", err)
				continue
			}
			if n, _ := result.RowsAffected(); n > 0 {
				slog.Debug("deleted expired pastes", "rows_deleted", n)
			}

		case <-vacuumTicker.C:
			// vacuumDB acquires s.mu internally — do NOT hold it here.
			s.vacuumDB(false, 10000)

		case <-fullVacuumTicker.C:
			s.vacuumDB(true, 0)

		case <-walCheckTicker.C:
			// walFileSize does not need the mutex (stat is not a DB operation).
			size := s.walFileSize()
			if size > 128*1024*1024 { // 128 MB threshold
				slog.Info("WAL file large, checkpointing", "size_mb", size/1024/1024)
				_, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
				if err != nil {
					slog.Error("WAL checkpoint failed", "err", err)
				}
			}

		case <-s.stop:
			return
		}
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
		SELECT data, expire_at FROM pastes
		WHERE id = $1 AND (expire_at IS NULL OR expire_at > NOW())`, key)

	var raw string
	var expireAt *time.Time
	if err := row.Scan(&raw, &expireAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	paste, err := unmarshalPaste([]byte(raw))
	if err != nil {
		return nil, err
	}
	paste.ExpireAt = expireAt
	return paste, nil
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
		RETURNING data, expire_at`, key)

	var raw string
	var expireAt *time.Time
	if err := row.Scan(&raw, &expireAt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	paste, err := unmarshalPaste([]byte(raw))
	if err != nil {
		return nil, err
	}
	paste.ExpireAt = expireAt
	return paste, nil
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

	// Pipeline GET + TTL together — two round-trips would be a race.
	pipe := s.client.Pipeline()
	getCmd := pipe.Get(ctx, key)
	ttlCmd := pipe.TTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	b, err := getCmd.Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	paste, err := unmarshalPaste(b)
	if err != nil {
		return nil, err
	}
	// TTL > 0 means the key has an expiry; -1 = no expiry, -2 = key gone.
	if ttl := ttlCmd.Val(); ttl > 0 {
		t := time.Now().Add(ttl)
		paste.ExpireAt = &t
	}
	return paste, nil
}

func (s *RedisStorage) Delete(key string) error {
	return s.client.Del(context.Background(), key).Err()
}

// GetAndDelete uses a Lua script to atomically read PTTL then GETDEL.
// PTTL must be called BEFORE GETDEL because GETDEL removes the key and its TTL.
func (s *RedisStorage) GetAndDelete(key string) (*PasteData, error) {
	ctx := context.Background()

	res, err := burnScript.Run(ctx, s.client, []string{key}).Slice()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // key did not exist
		}
		return nil, err
	}

	// res[0] is the serialised paste (string), res[1] is PTTL in milliseconds.
	// Lua `not val` branch returns {false, 0}; false becomes nil in Go's
	// []interface{} so the type assertion below correctly falls through.
	raw, ok := res[0].(string)
	if !ok || raw == "" {
		return nil, nil
	}

	paste, err := unmarshalPaste([]byte(raw))
	if err != nil {
		return nil, err
	}

	// PTTL returns -1 (no expiry) or -2 (key gone) or ms remaining.
	if pttl, ok := res[1].(int64); ok && pttl > 0 {
		t := time.Now().Add(time.Duration(pttl) * time.Millisecond)
		paste.ExpireAt = &t
	}

	return paste, nil
}

func (s *RedisStorage) Close() error { return s.client.Close() }

// =============================================================================
// Backend selector
// =============================================================================

func newStorage(cfg *Settings) Storage {
	if cfg.RedisURL != "" {
		s, err := newRedisStorage(cfg.RedisURL)
		if err == nil {
			slog.Info("using Redis backend")
			return s
		}
		slog.Warn("Redis unavailable, falling back", "err", err)
	}

	if cfg.PostgresURL != "" {
		s, err := newPostgresStorage(cfg.PostgresURL)
		if err == nil {
			slog.Info("using PostgreSQL backend")
			return s
		}
		slog.Warn("PostgreSQL unavailable, falling back", "err", err)
	}

	s, err := newSQLiteStorage(cfg.SQLitePath, cfg)
	if err != nil {
		slog.Error("SQLite init failed", "err", err)
		os.Exit(1)
	}
	slog.Info("using SQLite backend", "path", cfg.SQLitePath)
	return s
}