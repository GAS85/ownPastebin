package main

import (
	"bytes"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func newTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()
	f, err := os.CreateTemp("", "pastebin-storage-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)

	s, err := newSQLiteStorage(path, &Settings{SQLitePageSize: 0})
	if err != nil {
		t.Fatalf("newSQLiteStorage: %v", err)
	}
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
		os.Remove(path)
	})
	return s
}

func TestStorageSaveAndGet(t *testing.T) {
	s := newTestStorage(t)

	// Content is []byte — pass raw bytes, not a base64 string.
	want := []byte("hello")
	paste := &PasteData{Content: want, Lang: "text"}
	if err := s.Save("key1", paste, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get("key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if !bytes.Equal(got.Content, want) {
		t.Fatalf("content mismatch: got %q, want %q", got.Content, want)
	}
}

func TestStorageDelete(t *testing.T) {
	s := newTestStorage(t)

	s.Save("key2", &PasteData{Content: []byte("x")}, 0)
	s.Delete("key2")

	got, err := s.Get("key2")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestStorageTTLExpiry(t *testing.T) {
	s := newTestStorage(t)

	s.Save("ttl1", &PasteData{Content: []byte("temp")}, 1*time.Second)

	// Must exist immediately.
	got, _ := s.Get("ttl1")
	if got == nil {
		t.Fatal("paste should exist before TTL expires")
	}

	time.Sleep(2 * time.Second)

	got, err := s.Get("ttl1")
	if err != nil {
		t.Fatalf("Get after expiry: %v", err)
	}
	if got != nil {
		t.Fatal("paste should be nil after TTL expires")
	}
}

func TestStorageGetAndDeleteAtomic(t *testing.T) {
	s := newTestStorage(t)

	s.Save("burn1", &PasteData{Content: []byte("burnme"), Burn: true}, 0)

	got, err := s.GetAndDelete("burn1")
	if err != nil || got == nil {
		t.Fatalf("GetAndDelete: err=%v got=%v", err, got)
	}

	// Second call must return nil — the row is gone.
	got, err = s.GetAndDelete("burn1")
	if err != nil {
		t.Fatalf("second GetAndDelete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil on second GetAndDelete")
	}
}

func TestStorageMissingKey(t *testing.T) {
	s := newTestStorage(t)

	got, err := s.Get("doesnotexist")
	if err != nil {
		t.Fatalf("Get missing key: unexpected error %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestStorageExpireAtPopulated(t *testing.T) {
	s := newTestStorage(t)

	s.Save("exp1", &PasteData{Content: []byte("data")}, 1*time.Hour)

	got, err := s.Get("exp1")
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v got=%v", err, got)
	}
	if got.ExpireAt == nil {
		t.Fatal("ExpireAt should be set for paste with TTL")
	}
	if got.ExpireAt.Before(time.Now()) {
		t.Fatal("ExpireAt is in the past for a 1h TTL paste")
	}
}

func TestStorageNoExpireAtForPermanent(t *testing.T) {
	s := newTestStorage(t)

	s.Save("perm1", &PasteData{Content: []byte("permanent")}, 0)

	got, err := s.Get("perm1")
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v got=%v", err, got)
	}
	if got.ExpireAt != nil {
		t.Fatalf("ExpireAt should be nil for permanent paste, got %v", got.ExpireAt)
	}
}

func TestApplyPageSizeValidAndInvalid(t *testing.T) {
	f, err := os.CreateTemp("", "pastebin-page-size-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := applyPageSize(db, 1024); err != nil {
		t.Fatalf("expected valid page size to succeed, got %v", err)
	}

	if err := applyPageSize(db, 123); err != nil {
		t.Fatalf("expected invalid page size to be ignored, got %v", err)
	}
}

func TestEnsureIncrementalVacuumAlreadyIncremental(t *testing.T) {
	f, err := os.CreateTemp("", "pastebin-vacuum-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`PRAGMA auto_vacuum=2`); err != nil {
		t.Fatalf("set auto_vacuum failed: %v", err)
	}

	if err := ensureIncrementalVacuum(db); err != nil {
		t.Fatalf("ensureIncrementalVacuum failed: %v", err)
	}
}

func TestEnsureIncrementalVacuumMigrates(t *testing.T) {
	f, err := os.CreateTemp("", "pastebin-vacuum-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	defer os.Remove(path)

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureIncrementalVacuum(db); err != nil {
		t.Fatalf("ensureIncrementalVacuum migrate failed: %v", err)
	}
}

func TestWalFileSizeAndVacuumDB(t *testing.T) {
	s := newTestStorage(t)
	if err := s.Save("vacuum-key", &PasteData{Content: []byte("data")}, 0); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if s.walFileSize() == 0 {
		t.Fatal("expected WAL file to exist and have non-zero size")
	}
	if err := s.Delete("vacuum-key"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// The database may or may not expose free pages, but vacuumDB should never panic.
	s.vacuumDB(false, 1)
}

func TestCloseReclaimsFreelistOrClosesCleanly(t *testing.T) {
	f, err := os.CreateTemp("", "pastebin-storage-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	defer os.Remove(path)

	s, err := newSQLiteStorage(path, &Settings{SQLitePageSize: 0})
	if err != nil {
		t.Fatalf("newSQLiteStorage: %v", err)
	}

	if err := s.Save("close-key", &PasteData{Content: []byte("x")}, 0); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if err := s.Delete("close-key"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestSQLiteStatsReturnsCounts(t *testing.T) {
	s := newTestStorage(t)
	if err := s.Save("stat-key", &PasteData{Content: []byte("data"), Burn: true, Protected: true}, 0); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	st := s.Stats()
	if st.Backend != "sqlite" || st.Total != 1 || st.BurnOnRead != 1 || st.Protected != 1 {
		t.Fatalf("unexpected stats: %+v", st)
	}
	if st.DBFileBytes == 0 {
		t.Fatal("expected DBFileBytes to be populated")
	}
}

func TestStoragePeekMetaExpiredDeletes(t *testing.T) {
	s := newTestStorage(t)
	if err := s.Save("expire-meta", &PasteData{Content: []byte("gone")}, 1*time.Second); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	time.Sleep(2 * time.Second)
	got, err := s.PeekMeta("expire-meta")
	if err != nil {
		t.Fatalf("PeekMeta: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for expired paste")
	}
}

func TestNewStorageFallsBackToSQLite(t *testing.T) {
	f, err := os.CreateTemp("", "pastebin-storage-fallback-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	defer os.Remove(path)

	cfg := &Settings{RedisURL: "redis://127.0.0.1:1", SQLitePath: path, MaxParallelUploads: 1}
	store := newStorage(cfg)
	defer store.Close()
	if _, ok := store.(*SQLiteStorage); !ok {
		t.Fatalf("expected SQLiteStorage fallback, got %T", store)
	}
}

func TestBoolIntConversions(t *testing.T) {
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatal("unexpected boolToInt results")
	}
	if !intToBool(1) || intToBool(0) {
		t.Fatal("unexpected intToBool results")
	}
}

func TestRedisValueHelpers(t *testing.T) {
	if got := redisKey("abc"); got != "paste:abc" {
		t.Fatalf("unexpected redis key: %q", got)
	}
	if got := valToString(nil); got != "" {
		t.Fatalf("expected empty string for nil")
	}
	if got := valToInt("42"); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := valToInt(nil); got != 0 {
		t.Fatalf("expected 0 for nil")
	}
	if got, err := valToBytes([]byte("x")); err != nil || string(got) != "x" {
		t.Fatalf("expected raw bytes, got %v err=%v", got, err)
	}
}

func TestLogStatsLogsSqliteExtraFields(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(&textHandler{w: &buf, level: slog.LevelDebug, dateFormat: "2006-01-02"}))
	defer slog.SetDefault(orig)

	logStats(StorageStats{Backend: "sqlite", DBFileBytes: 123, WALFileBytes: 456, PageSize: 4096, PageCount: 2, FreePages: 1})
	out := buf.String()
	if !strings.Contains(out, "storage stats") || !strings.Contains(out, "sqlite file stats") {
		t.Fatalf("unexpected log output: %q", out)
	}
}
