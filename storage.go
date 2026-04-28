package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/net/context"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// ErrSlugConflict is returned by Save when the generated ID already exists. The caller should generate a new ID and retry.
var ErrSlugConflict = errors.New("slug already exists")

// =============================================================================
// DATA MODEL
// =============================================================================

// PasteData is the internal representation – Content is raw bytes
// (plaintext or ciphertext, depending on Encrypted flag).

type PasteData struct {
	Content      []byte
	Burn         bool
	Encrypted    bool
	E2EEncrypted bool
	Protected    bool // DELETE is rejected with 403 when true (requires ProtectedPasteEnabled in config)
	Lang         string
	ExpireAt     *time.Time
}

// StorageStats holds counts collected at startup — logged once by newStorage.
// All counts reflect the state at the moment of the query; the DB may change
// immediately after. Zero values mean the backend could not determine the count.
type StorageStats struct {
	Backend      string // "sqlite", "postgres", "redis"
	Total        int64  // active (non-expired) pastes
	Permanent    int64  // pastes with no TTL
	Expiring     int64  // pastes with a TTL set
	BurnOnRead   int64  // burn=true pastes
	SSEncrypted  int64  // server-side encrypted
	E2EEncrypted int64  // client-side encrypted
	Protected    int64  // protected pastes (DELETE blocked)

	// SQLite-only — zero for other backends.
	DBFileBytes  int64
	WALFileBytes int64
	PageSize     int64
	PageCount    int64
	FreePages    int64
}

// logStats emits one INFO line per stat group so the log stays readable.
func logStats(st StorageStats) {
	slog.Info("storage stats",
		"backend", st.Backend,
		"total_active", st.Total,
		"permanent", st.Permanent,
		"expiring", st.Expiring,
		"burn_on_read", st.BurnOnRead,
		"ss_encrypted", st.SSEncrypted,
		"e2e_encrypted", st.E2EEncrypted,
		"protected", st.Protected,
	)
	if st.Backend == "sqlite" {
		slog.Info("sqlite file stats",
			"db_bytes", st.DBFileBytes,
			"wal_bytes", st.WALFileBytes,
			"page_size", st.PageSize,
			"page_count", st.PageCount,
			"free_pages", st.FreePages,
		)
	}
}

// Storage is the common interface all backends implement.
type Storage interface {
	Save(key string, data *PasteData, ttl time.Duration) error
	Get(key string) (*PasteData, error)
	PeekMeta(key string) (*PasteData, error) // returns metadata without Content
	Delete(key string) error
	// GetAndDelete atomically reads and removes — used for burn-on-read.
	GetAndDelete(key string) (*PasteData, error)
	// Stats collects startup statistics. Called once after the backend is initialized; errors are non-fatal and result in partial stats.
	Stats() StorageStats
	Close() error
}

// =============================================================================
// SQLite – stores content as BLOB, metadata in separate columns
// =============================================================================

type SQLiteStorage struct {
	db     *sql.DB
	dbPath string
	stop   chan struct{}
	wg     sync.WaitGroup
}

// helper conversions
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func intToBool(i int) bool {
	return i != 0
}

func newSQLiteStorage(path string, cfg *Settings) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	// Open connections scaled to MaxParallelUploads so writers do not queue
	// behind a fixed ceiling while many concurrent uploads are active.
	maxOpen := cfg.MaxParallelUploads + 4
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := applyPageSize(db, cfg.SQLitePageSize); err != nil {
		db.Close()
		return nil, err
	}

	// New schema: content BLOB, explicit columns for all fields
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS pastes (
		id            TEXT    PRIMARY KEY,
		content       BLOB    NOT NULL,
		burn          INTEGER NOT NULL DEFAULT 0,
		encrypted     INTEGER NOT NULL DEFAULT 0,
		e2e_encrypted INTEGER NOT NULL DEFAULT 0,
		lang          TEXT    NOT NULL DEFAULT 'text',
		expire_at     INTEGER,
		protected     INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Migration: add 'protected' column to existing databases that pre-date this field.
	// SQLite does not support ADD COLUMN IF NOT EXISTS, so we swallow the
	// "duplicate column name" error that fires when the column already exists.
	_, err = db.Exec(`ALTER TABLE pastes ADD COLUMN protected INTEGER NOT NULL DEFAULT 0`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		db.Close()
		return nil, fmt.Errorf("migration: add protected column: %w", err)
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

// Stats collects SQLite statistics in a single query plus three PRAGMA calls.
func (s *SQLiteStorage) Stats() StorageStats {
	st := StorageStats{Backend: "sqlite"}
	now := time.Now().Unix()

	// All counts in one pass — no table scan of content BLOB.
	row := s.db.QueryRow(`
		SELECT
			COUNT(*)                                                   AS total,
			SUM(CASE WHEN expire_at IS NULL     THEN 1 ELSE 0 END)     AS permanent,
			SUM(CASE WHEN expire_at IS NOT NULL THEN 1 ELSE 0 END)     AS expiring,
			SUM(CASE WHEN burn = 1              THEN 1 ELSE 0 END)     AS burn,
			SUM(CASE WHEN encrypted = 1         THEN 1 ELSE 0 END)     AS ss_enc,
			SUM(CASE WHEN e2e_encrypted = 1     THEN 1 ELSE 0 END)     AS e2e_enc,
			SUM(CASE WHEN protected = 1         THEN 1 ELSE 0 END)     AS prot
		FROM pastes
		WHERE expire_at IS NULL OR expire_at > ?`, now)

	var perm, expiring, burn, ssEnc, e2eEnc, prot sql.NullInt64
	if err := row.Scan(&st.Total, &perm, &expiring, &burn, &ssEnc, &e2eEnc, &prot); err != nil {
		slog.Warn("sqlite stats query failed", "err", err)
	}
	st.Permanent    = perm.Int64
	st.Expiring     = expiring.Int64
	st.BurnOnRead   = burn.Int64
	st.SSEncrypted  = ssEnc.Int64
	st.E2EEncrypted = e2eEnc.Int64
	st.Protected    = prot.Int64

	// File sizes from the OS — no DB connection needed.
	if fi, err := os.Stat(s.dbPath); err == nil {
		st.DBFileBytes = fi.Size()
	}
	if fi, err := os.Stat(s.dbPath + "-wal"); err == nil {
		st.WALFileBytes = fi.Size()
	}

	// PRAGMA calls are cheap single-row reads.
	s.db.QueryRow(`PRAGMA page_size`).Scan(&st.PageSize)
	s.db.QueryRow(`PRAGMA page_count`).Scan(&st.PageCount)
	s.db.QueryRow(`PRAGMA freelist_count`).Scan(&st.FreePages)

	return st
}

// Save inserts a new paste. It uses INSERT OR IGNORE so that a slug collision returns ErrSlugConflict instead of silently overwriting an existing paste.  The caller (handleCreatePaste) retries with a fresh ID.
func (s *SQLiteStorage) Save(key string, d *PasteData, ttl time.Duration) error {
	var expireAt *int64
	if ttl > 0 {
		t := time.Now().Add(ttl).Unix()
		expireAt = &t
	}
	res, err := s.db.Exec(`
		INSERT OR IGNORE INTO pastes
		(id, content, burn, encrypted, e2e_encrypted, lang, expire_at, protected)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		key, d.Content, boolToInt(d.Burn), boolToInt(d.Encrypted),
		boolToInt(d.E2EEncrypted), d.Lang, expireAt, boolToInt(d.Protected),
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSlugConflict
	}
	return nil
}

func (s *SQLiteStorage) PeekMeta(key string) (*PasteData, error) {
	row := s.db.QueryRow(`
		SELECT burn, encrypted, e2e_encrypted, lang, expire_at, protected
		FROM pastes WHERE id = ?`, key)

	var burnInt, encInt, e2eInt, protInt int
	var lang string
	var expireAt *int64
	if err := row.Scan(&burnInt, &encInt, &e2eInt, &lang, &expireAt, &protInt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	// Check expiry without loading content
	if expireAt != nil && time.Now().Unix() >= *expireAt {
		s.db.Exec(`DELETE FROM pastes WHERE id = ?`, key) // clean up
		return nil, nil
	}
	paste := &PasteData{
		Content:      nil, // metadata only
		Burn:         intToBool(burnInt),
		Encrypted:    intToBool(encInt),
		E2EEncrypted: intToBool(e2eInt),
		Protected:    intToBool(protInt),
		Lang:         lang,
	}
	if expireAt != nil {
		t := time.Unix(*expireAt, 0)
		paste.ExpireAt = &t
	}
	return paste, nil
}

func (s *SQLiteStorage) Get(key string) (*PasteData, error) {
	row := s.db.QueryRow(`
		SELECT content, burn, encrypted, e2e_encrypted, lang, expire_at, protected
		FROM pastes WHERE id = ?`, key)

	var content []byte
	var burnInt, encInt, e2eInt, protInt int
	var lang string
	var expireAt *int64
	if err := row.Scan(&content, &burnInt, &encInt, &e2eInt, &lang, &expireAt, &protInt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if expireAt != nil && time.Now().Unix() >= *expireAt {
		s.db.Exec(`DELETE FROM pastes WHERE id = ?`, key)
		return nil, nil
	}
	paste := &PasteData{
		Content:      content,
		Burn:         intToBool(burnInt),
		Encrypted:    intToBool(encInt),
		E2EEncrypted: intToBool(e2eInt),
		Protected:    intToBool(protInt),
		Lang:         lang,
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
		RETURNING content, burn, encrypted, e2e_encrypted, lang, expire_at, protected`, key, time.Now().Unix())

	var content []byte
	var burnInt, encInt, e2eInt, protInt int
	var lang string
	var expireAt *int64
	if err := row.Scan(&content, &burnInt, &encInt, &e2eInt, &lang, &expireAt, &protInt); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	paste := &PasteData{
		Content:      content,
		Burn:         intToBool(burnInt),
		Encrypted:    intToBool(encInt),
		E2EEncrypted: intToBool(e2eInt),
		Protected:    intToBool(protInt),
		Lang:         lang,
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
			s.vacuumDB(false, 10000)

		case <-fullVacuumTicker.C:
			s.vacuumDB(true, 0)

		case <-walCheckTicker.C:
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
// PostgreSQL – stores content as BYTEA
// =============================================================================

type PostgresStorage struct {
	db   *sql.DB
	stop chan struct{}
	wg   sync.WaitGroup
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
		id            TEXT    PRIMARY KEY,
		content       BYTEA   NOT NULL,
		burn          BOOLEAN NOT NULL DEFAULT FALSE,
		encrypted     BOOLEAN NOT NULL DEFAULT FALSE,
		e2e_encrypted BOOLEAN NOT NULL DEFAULT FALSE,
		lang          TEXT    NOT NULL DEFAULT 'text',
		expire_at     TIMESTAMPTZ,
		protected     BOOLEAN NOT NULL DEFAULT FALSE
	)`)
	if err != nil {
		return nil, err
	}

	// Migration: add 'protected' column to existing databases.
	// ADD COLUMN IF NOT EXISTS is idempotent on PostgreSQL ≥ 9.6.
	_, err = db.Exec(`ALTER TABLE pastes ADD COLUMN IF NOT EXISTS protected BOOLEAN NOT NULL DEFAULT FALSE`)
	if err != nil {
		return nil, fmt.Errorf("migration: add protected column: %w", err)
	}

	s := &PostgresStorage{
		db:   db,
		stop: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.cleanupLoop()
	return s, nil
}

// cleanupLoop deletes expired rows from PostgreSQL hourly.
func (s *PostgresStorage) cleanupLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			result, err := s.db.Exec(
				`DELETE FROM pastes WHERE expire_at IS NOT NULL AND expire_at < NOW()`,
			)
			if err != nil {
				slog.Error("postgres cleanup failed", "err", err)
				continue
			}
			if n, _ := result.RowsAffected(); n > 0 {
				slog.Debug("postgres: deleted expired pastes", "rows_deleted", n)
			}
		case <-s.stop:
			return
		}
	}
}

// Stats collects Postgres statistics in a single query.
func (s *PostgresStorage) Stats() StorageStats {
	st := StorageStats{Backend: "postgres"}

	row := s.db.QueryRow(`
		SELECT
			COUNT(*)                                                          AS total,
			COUNT(*) FILTER (WHERE expire_at IS NULL)                        AS permanent,
			COUNT(*) FILTER (WHERE expire_at IS NOT NULL)                    AS expiring,
			COUNT(*) FILTER (WHERE burn = TRUE)                              AS burn,
			COUNT(*) FILTER (WHERE encrypted = TRUE)                         AS ss_enc,
			COUNT(*) FILTER (WHERE e2e_encrypted = TRUE)                     AS e2e_enc,
			COUNT(*) FILTER (WHERE protected = TRUE)                         AS prot
		FROM pastes
		WHERE expire_at IS NULL OR expire_at > NOW()`)

	if err := row.Scan(&st.Total, &st.Permanent, &st.Expiring,
		&st.BurnOnRead, &st.SSEncrypted, &st.E2EEncrypted, &st.Protected); err != nil {
		slog.Warn("postgres stats query failed", "err", err)
	}
	return st
}

// Save inserts a new paste. Uses ON CONFLICT DO NOTHING so that a collision returns ErrSlugConflict rather than overwriting an existing paste.
func (s *PostgresStorage) Save(key string, d *PasteData, ttl time.Duration) error {
	var expireAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expireAt = &t
	}
	res, err := s.db.Exec(`
		INSERT INTO pastes (id, content, burn, encrypted, e2e_encrypted, lang, expire_at, protected)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING`,
		key, d.Content, d.Burn, d.Encrypted, d.E2EEncrypted, d.Lang, expireAt, d.Protected,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSlugConflict
	}
	return nil
}

func (s *PostgresStorage) PeekMeta(key string) (*PasteData, error) {
	row := s.db.QueryRow(`
		SELECT burn, encrypted, e2e_encrypted, lang, expire_at, protected
		FROM pastes
		WHERE id = $1 AND (expire_at IS NULL OR expire_at > NOW())`, key)

	var burn, encrypted, e2eEncrypted, protected bool
	var lang string
	var expireAt *time.Time
	if err := row.Scan(&burn, &encrypted, &e2eEncrypted, &lang, &expireAt, &protected); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &PasteData{
		Content:      nil,
		Burn:         burn,
		Encrypted:    encrypted,
		E2EEncrypted: e2eEncrypted,
		Protected:    protected,
		Lang:         lang,
		ExpireAt:     expireAt,
	}, nil
}

func (s *PostgresStorage) Get(key string) (*PasteData, error) {
	row := s.db.QueryRow(`
		SELECT content, burn, encrypted, e2e_encrypted, lang, expire_at, protected
		FROM pastes
		WHERE id = $1 AND (expire_at IS NULL OR expire_at > NOW())`, key)

	var content []byte
	var burn, encrypted, e2eEncrypted, protected bool
	var lang string
	var expireAt *time.Time
	if err := row.Scan(&content, &burn, &encrypted, &e2eEncrypted, &lang, &expireAt, &protected); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &PasteData{
		Content:      content,
		Burn:         burn,
		Encrypted:    encrypted,
		E2EEncrypted: e2eEncrypted,
		Protected:    protected,
		Lang:         lang,
		ExpireAt:     expireAt,
	}, nil
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
		RETURNING content, burn, encrypted, e2e_encrypted, lang, expire_at, protected`, key)

	var content []byte
	var burn, encrypted, e2eEncrypted, protected bool
	var lang string
	var expireAt *time.Time
	if err := row.Scan(&content, &burn, &encrypted, &e2eEncrypted, &lang, &expireAt, &protected); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &PasteData{
		Content:      content,
		Burn:         burn,
		Encrypted:    encrypted,
		E2EEncrypted: e2eEncrypted,
		Protected:    protected,
		Lang:         lang,
		ExpireAt:     expireAt,
	}, nil
}

func (s *PostgresStorage) Close() error {
	close(s.stop)
	s.wg.Wait()
	return s.db.Close()
}

// =============================================================================
// Redis – stores pastes as Hashes (binary-safe)
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

// redisPastePrefix is prepended to every paste key so Stats() SCAN only
// matches paste keys even when the Redis instance is shared with other apps.
// Save() uses the same prefix; Get/Delete/GetAndDelete strip it via the caller
// passing the already-prefixed key, since handleCreatePaste controls the ID.
const redisPastePrefix = "paste:"

// redisKey returns the full storage key for a paste ID.
func redisKey(id string) string { return redisPastePrefix + id }

// Stats scans paste keys to collect counts. Runs in a background goroutine
// and logs results when done so startup is never blocked.
//
// Three fixes over the naive approach:
//  1. SCAN uses the "paste:*" pattern — avoids counting keys from other apps
//     sharing the same Redis instance.
//  2. HMGet and TTL are pipelined per batch — one round-trip per SCAN page
//     instead of one round-trip per key (O(N) → O(N/batchSize)).
//  3. Background execution — does not delay startup.
func (s *RedisStorage) Stats() StorageStats {
	go func() {
		st := StorageStats{Backend: "redis"}

		// 30 seconds is generous enough for large keyspaces but still bounded
		// so the goroutine does not leak indefinitely.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var cursor uint64
		for {
			keys, next, err := s.client.Scan(ctx, cursor, redisPastePrefix+"*", 200).Result()
			if err != nil {
				slog.Warn("redis stats scan failed", "err", err)
				break
			}

			if len(keys) > 0 {
				// Pipeline all HMGet + TTL calls for the current batch —
				// one round-trip instead of len(keys) separate round-trips.
				pipe := s.client.Pipeline()
				hmgetCmds := make([]*redis.SliceCmd, len(keys))
				ttlCmds := make([]*redis.DurationCmd, len(keys))
				for i, key := range keys {
					hmgetCmds[i] = pipe.HMGet(ctx, key, "burn", "enc", "e2e", "prot")
					ttlCmds[i] = pipe.TTL(ctx, key)
				}
				if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
					slog.Warn("redis stats pipeline failed", "err", err)
					break
				}

				for i := range keys {
					vals := hmgetCmds[i].Val()
					// vals[0] == nil: key expired between SCAN and pipeline exec,
					// OR the hash has no "burn" field (partial write from a failed
					// Save after the first HSETNX pipeline but before the second).
					// Either way, skip — partial entries are not valid pastes.
					if len(vals) < 4 || vals[0] == nil {
						continue
					}
					st.Total++
					if intToBool(valToInt(vals[0])) {
						st.BurnOnRead++
					}
					if intToBool(valToInt(vals[1])) {
						st.SSEncrypted++
					}
					if intToBool(valToInt(vals[2])) {
						st.E2EEncrypted++
					}
					if intToBool(valToInt(vals[3])) {
						st.Protected++
					}
					if ttlCmds[i].Val() > 0 {
						st.Expiring++
					} else {
						st.Permanent++
					}
				}
			}

			cursor = next
			if cursor == 0 {
				break
			}
		}
		logStats(st)
	}()

	// Return an empty placeholder immediately — logStats fires in the background.
	return StorageStats{Backend: "redis"}
}

func (s *RedisStorage) Save(key string, d *PasteData, ttl time.Duration) error {
	ctx := context.Background()
	rkey := redisKey(key)

	// HSETNX on "content" as collision gate: if the field already exists,
	// the slug was taken — return ErrSlugConflict without overwriting.
	pipe := s.client.TxPipeline()
	hsetnx := pipe.HSetNX(ctx, rkey, "content", d.Content)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	if !hsetnx.Val() {
		return ErrSlugConflict
	}

	// Key is new — write remaining fields and TTL.
	// A failure here leaves a partial hash (content only); Stats() detects
	// this via the nil "burn" field and skips the entry.
	pipe2 := s.client.TxPipeline()
	pipe2.HSet(ctx, rkey, map[string]interface{}{
		"burn": boolToInt(d.Burn),
		"enc":  boolToInt(d.Encrypted),
		"e2e":  boolToInt(d.E2EEncrypted),
		"prot": boolToInt(d.Protected),
		"lang": d.Lang,
	})
	if ttl > 0 {
		pipe2.Expire(ctx, rkey, ttl)
	}
	_, err = pipe2.Exec(ctx)
	return err
}

// PeekMeta fetches all metadata fields in a single HMGet round trip.
func (s *RedisStorage) PeekMeta(key string) (*PasteData, error) {
	ctx := context.Background()
	rkey := redisKey(key)
	// Single round trip: fetch burn, enc, e2e, prot, lang together.
	vals, err := s.client.HMGet(ctx, rkey, "burn", "enc", "e2e", "prot", "lang").Result()
	if err != nil {
		return nil, err
	}
	if vals[0] == nil {
		return nil, nil
	}
	p := &PasteData{
		Burn: intToBool(valToInt(vals[0])), Encrypted: intToBool(valToInt(vals[1])),
		E2EEncrypted: intToBool(valToInt(vals[2])), Protected: intToBool(valToInt(vals[3])),
		Lang: valToString(vals[4]),
	}
	if ttl := s.client.TTL(ctx, rkey).Val(); ttl > 0 {
		t := time.Now().Add(ttl)
		p.ExpireAt = &t
	}
	return p, nil
}

// Get retrieves a paste from Redis in two round trips: one HMGet for all
// fields (including content) and one TTL call.
func (s *RedisStorage) Get(key string) (*PasteData, error) {
	ctx := context.Background()
	rkey := redisKey(key)
	// Fetch all fields in one round trip.
	vals, err := s.client.HMGet(ctx, rkey, "content", "burn", "enc", "e2e", "prot", "lang").Result()
	if err != nil {
		return nil, err
	}
	if vals[0] == nil {
		return nil, nil  // key does not exist
	}
	content, err := valToBytes(vals[0])
	if err != nil {
		return nil, fmt.Errorf("redis Get decode content: %w", err)
	}
	p := &PasteData{
		Content: content, Burn: intToBool(valToInt(vals[1])),
		Encrypted: intToBool(valToInt(vals[2])), E2EEncrypted: intToBool(valToInt(vals[3])),
		Protected: intToBool(valToInt(vals[4])), Lang: valToString(vals[5]),
	}
	if ttl := s.client.TTL(ctx, rkey).Val(); ttl > 0 {
		t := time.Now().Add(ttl)
		p.ExpireAt = &t
	}
	return p, nil
}

func (s *RedisStorage) Delete(key string) error {
	return s.client.Del(context.Background(), redisKey(key)).Err()
}

func (s *RedisStorage) GetAndDelete(key string) (*PasteData, error) {
	ctx := context.Background()
	rkey := redisKey(key)

	pipe := s.client.TxPipeline()
	contentCmd := pipe.HGet(ctx, rkey, "content")
	burnCmd    := pipe.HGet(ctx, rkey, "burn")
	encCmd     := pipe.HGet(ctx, rkey, "enc")
	e2eCmd     := pipe.HGet(ctx, rkey, "e2e")
	protCmd    := pipe.HGet(ctx, rkey, "prot")
	langCmd    := pipe.HGet(ctx, rkey, "lang")
	ttlCmd     := pipe.TTL(ctx, rkey)
	pipe.Del(ctx, rkey)

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, err
	}

	content, err := contentCmd.Bytes()
	if err == redis.Nil {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	burn, _ := burnCmd.Int()
	enc, _  := encCmd.Int()
	e2e, _  := e2eCmd.Int()
	prot, _ := protCmd.Int()
	lang, _ := langCmd.Result()
	ttl     := ttlCmd.Val()

	p := &PasteData{
		Content:      content,
		Burn:         intToBool(burn),
		Encrypted:    intToBool(enc),
		E2EEncrypted: intToBool(e2e),
		Protected:    intToBool(prot),
		Lang:         lang,
	}

	if ttl > 0 {
		t := time.Now().Add(ttl)
		p.ExpireAt = &t
	}

	return p, nil
}

func (s *RedisStorage) Close() error {
	return s.client.Close()
}

// ---------------------------------------------------------------------------
// Redis HMGet value helpers
// ---------------------------------------------------------------------------

func valToString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func valToInt(v interface{}) int {
	s := valToString(v)
	if s == "" {
		return 0
	}
	n := 0
	fmt.Sscanf(s, "%d", &n)
	return n
}

func valToBytes(v interface{}) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	switch t := v.(type) {
	case string:
		return []byte(t), nil
	case []byte:
		return t, nil
	default:
		return []byte(fmt.Sprintf("%v", v)), nil
	}
}

// =============================================================================
// Backend selector
// =============================================================================

func newStorage(cfg *Settings) Storage {
	var store Storage

	if cfg.RedisURL != "" {
		s, err := newRedisStorage(cfg.RedisURL)
		if err == nil {
			slog.Info("using Redis backend")
			store = s
		} else {
			slog.Warn("Redis unavailable, falling back", "err", err)
		}
	}

	if store == nil && cfg.PostgresURL != "" {
		s, err := newPostgresStorage(cfg.PostgresURL)
		if err == nil {
			slog.Info("using PostgreSQL backend")
			store = s
		} else {
			slog.Warn("PostgreSQL unavailable, falling back", "err", err)
		}
	}

	if store == nil {
		s, err := newSQLiteStorage(cfg.SQLitePath, cfg)
		if err != nil {
			slog.Error("SQLite init failed", "err", err)
			os.Exit(1)
		}
		slog.Info("using SQLite backend", "path", cfg.SQLitePath)
		store = s
	}

	// Collect and log startup statistics — non-fatal, best-effort.
	logStats(store.Stats())

	return store
}