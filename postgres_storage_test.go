package main

import (
	"bytes"
	"database/sql"
	"os"
	"testing"
	"time"
)

// Test helpers for PostgreSQL
func NewTestPostgresStorage(t *testing.T) (Storage, func()) {
	t.Helper()
	// Use environment variable for test DSN, fallback to default
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/pastebin_test?sslmode=disable"
	}
	
	s, err := newPostgresStorage(dsn)
	if err != nil {
		t.Skipf("Skipping PostgreSQL tests: %v", err)
	}
	
	// Clean up any existing test data
	_, err = s.db.Exec(`DELETE FROM pastes WHERE id LIKE 'test-%'`)
	if err != nil {
		t.Logf("Warning: could not clean test data: %v", err)
	}
	
	cleanup := func() {
		// Clean up test data
		s.db.Exec(`DELETE FROM pastes WHERE id LIKE 'test-%'`)
		s.Close()
	}
	return s, cleanup
}

// PostgreSQL-specific tests
func TestPostgresSaveAndGet(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	want := []byte("hello")
	paste := &PasteData{Content: want, Lang: "text"}
	key := "test-pg-key1"
	if err := store.Save(key, paste, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer store.Delete(key)

	got, err := store.Get(key)
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

func TestPostgresDelete(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-key2"
	if err := store.Save(key, &PasteData{Content: []byte("x")}, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestPostgresTTLExpiry(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-ttl1"
	if err := store.Save(key, &PasteData{Content: []byte("temp")}, 1*time.Second); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer store.Delete(key)

	// Must exist immediately
	got, _ := store.Get(key)
	if got == nil {
		t.Fatal("paste should exist before TTL expires")
	}

	time.Sleep(2 * time.Second)

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get after expiry: %v", err)
	}
	if got != nil {
		t.Fatal("paste should be nil after TTL expires")
	}
}

func TestPostgresGetAndDeleteAtomic(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-burn1"
	if err := store.Save(key, &PasteData{Content: []byte("burnme"), Burn: true}, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.GetAndDelete(key)
	if err != nil || got == nil {
		t.Fatalf("GetAndDelete: err=%v got=%v", err, got)
	}

	// Second call must return nil — the row is gone
	got, err = store.GetAndDelete(key)
	if err != nil {
		t.Fatalf("second GetAndDelete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil on second GetAndDelete")
	}
}

func TestPostgresMissingKey(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	got, err := store.Get("test-pg-doesnotexist")
	if err != nil {
		t.Fatalf("Get missing key: unexpected error %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestPostgresExpireAtPopulated(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-exp1"
	if err := store.Save(key, &PasteData{Content: []byte("data")}, 1*time.Hour); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer store.Delete(key)

	got, err := store.Get(key)
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

func TestPostgresNoExpireAtForPermanent(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-perm1"
	if err := store.Save(key, &PasteData{Content: []byte("permanent")}, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer store.Delete(key)

	got, err := store.Get(key)
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v got=%v", err, got)
	}
	if got.ExpireAt != nil {
		t.Fatalf("ExpireAt should be nil for permanent paste, got %v", got.ExpireAt)
	}
}

func TestPostgresPeekMeta(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-meta1"
	// Create a paste with various flags
	paste := &PasteData{
		Content:      []byte("secret data"),
		Burn:         true,
		Encrypted:    true,
		E2EEncrypted: false,
		Protected:    true,
		Lang:         "text",
	}
	if err := store.Save(key, paste, 1*time.Hour); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer store.Delete(key)

	// Peek should return metadata without content
	meta, err := store.PeekMeta(key)
	if err != nil {
		t.Fatalf("PeekMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("PeekMeta returned nil")
	}
	if meta.Content != nil {
		t.Fatal("PeekMeta should not return content")
	}
	if !meta.Burn || !meta.Encrypted || meta.E2EEncrypted || !meta.Protected {
		t.Fatal("metadata flags mismatch")
	}
	if meta.Lang != "text" {
		t.Fatalf("lang mismatch: got %q, want text", meta.Lang)
	}
	if meta.ExpireAt == nil {
		t.Fatal("ExpireAt should be set")
	}
}

func TestPostgresPeekMetaExpired(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-expire-meta"
	if err := store.Save(key, &PasteData{Content: []byte("gone")}, 1*time.Second); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	defer store.Delete(key)
	
	time.Sleep(2 * time.Second)
	got, err := store.PeekMeta(key)
	if err != nil {
		t.Fatalf("PeekMeta: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for expired paste")
	}
}

func TestPostgresSlugConflict(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-conflict"
	// First save
	paste := &PasteData{Content: []byte("first")}
	if err := store.Save(key, paste, 0); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	defer store.Delete(key)

	// Second save with same key should return ErrSlugConflict
	paste2 := &PasteData{Content: []byte("second")}
	err := store.Save(key, paste2, 0)
	if err != ErrSlugConflict {
		t.Fatalf("expected ErrSlugConflict, got %v", err)
	}

	// Original content should remain unchanged
	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got.Content, []byte("first")) {
		t.Fatalf("content was overwritten: got %q, want first", got.Content)
	}
}

func TestPostgresStats(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	// Clean up any existing test data
	testKeys := []string{"test-pg-stat1", "test-pg-stat2", "test-pg-stat3", "test-pg-stat4", "test-pg-stat5"}
	for _, key := range testKeys {
		store.Delete(key)
	}

	// Create various types of pastes
	testCases := []struct {
		key   string
		paste *PasteData
		ttl   time.Duration
	}{
		{"test-pg-stat1", &PasteData{Content: []byte("permanent")}, 0},
		{"test-pg-stat2", &PasteData{Content: []byte("expiring"), Burn: true}, 1 * time.Hour},
		{"test-pg-stat3", &PasteData{Content: []byte("encrypted"), Encrypted: true}, 0},
		{"test-pg-stat4", &PasteData{Content: []byte("e2e"), E2EEncrypted: true}, 0},
		{"test-pg-stat5", &PasteData{Content: []byte("protected"), Protected: true}, 0},
	}

	for _, tc := range testCases {
		if err := store.Save(tc.key, tc.paste, tc.ttl); err != nil {
			t.Fatalf("Save %s: %v", tc.key, err)
		}
	}
	defer func() {
		for _, key := range testKeys {
			store.Delete(key)
		}
	}()

	// Get stats
	stats := store.Stats()
	
	// Basic validation
	if stats.Backend != "postgres" {
		t.Errorf("expected backend postgres, got %s", stats.Backend)
	}
	if stats.Total < 5 {
		t.Errorf("Total count too low: got %d, want at least 5", stats.Total)
	}
	if stats.Permanent < 4 {
		t.Errorf("Permanent count too low: got %d, want at least 4", stats.Permanent)
	}
	if stats.Expiring < 1 {
		t.Errorf("Expiring count too low: got %d, want at least 1", stats.Expiring)
	}
	if stats.BurnOnRead < 1 {
		t.Errorf("BurnOnRead count too low: got %d, want at least 1", stats.BurnOnRead)
	}
	if stats.SSEncrypted < 1 {
		t.Errorf("SSEncrypted count too low: got %d, want at least 1", stats.SSEncrypted)
	}
	if stats.E2EEncrypted < 1 {
		t.Errorf("E2EEncrypted count too low: got %d, want at least 1", stats.E2EEncrypted)
	}
	if stats.Protected < 1 {
		t.Errorf("Protected count too low: got %d, want at least 1", stats.Protected)
	}
}

func TestPostgresAllDataTypes(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	// Test all boolean flags combinations
	testCases := []struct {
		name      string
		burn      bool
		encrypted bool
		e2e       bool
		protected bool
		lang      string
	}{
		{"all-false", false, false, false, false, "text"},
		{"all-true", true, true, true, true, "go"},
		{"mixed1", true, false, true, false, "python"},
		{"mixed2", false, true, false, true, "javascript"},
	}

	for _, tc := range testCases {
		key := "test-pg-types-" + tc.name
		paste := &PasteData{
			Content:      []byte(tc.name),
			Burn:         tc.burn,
			Encrypted:    tc.encrypted,
			E2EEncrypted: tc.e2e,
			Protected:    tc.protected,
			Lang:         tc.lang,
		}
		
		if err := store.Save(key, paste, 0); err != nil {
			t.Fatalf("Save %s: %v", tc.name, err)
		}
		defer store.Delete(key)
		
		got, err := store.Get(key)
		if err != nil {
			t.Fatalf("Get %s: %v", tc.name, err)
		}
		if got == nil {
			t.Fatalf("Get %s returned nil", tc.name)
		}
		
		if got.Burn != tc.burn {
			t.Errorf("%s: Burn = %v, want %v", tc.name, got.Burn, tc.burn)
		}
		if got.Encrypted != tc.encrypted {
			t.Errorf("%s: Encrypted = %v, want %v", tc.name, got.Encrypted, tc.encrypted)
		}
		if got.E2EEncrypted != tc.e2e {
			t.Errorf("%s: E2EEncrypted = %v, want %v", tc.name, got.E2EEncrypted, tc.e2e)
		}
		if got.Protected != tc.protected {
			t.Errorf("%s: Protected = %v, want %v", tc.name, got.Protected, tc.protected)
		}
		if got.Lang != tc.lang {
			t.Errorf("%s: Lang = %q, want %q", tc.name, got.Lang, tc.lang)
		}
	}
}

func TestPostgresLargeContent(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	key := "test-pg-large"
	// Test with 1MB of data
	largeContent := bytes.Repeat([]byte("A"), 1024*1024)
	paste := &PasteData{Content: largeContent, Lang: "text"}
	
	if err := store.Save(key, paste, 0); err != nil {
		t.Fatalf("Save large content: %v", err)
	}
	defer store.Delete(key)
	
	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get large content: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil for large content")
	}
	if !bytes.Equal(got.Content, largeContent) {
		t.Fatal("large content mismatch")
	}
}

func TestPostgresConcurrentOperations(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	// Test concurrent saves with different keys
	const n = 10
	done := make(chan bool, n)
	
	for i := 0; i < n; i++ {
		go func(idx int) {
			key := "test-pg-concurrent-" + string(rune('0'+idx))
			paste := &PasteData{Content: []byte("data"), Lang: "text"}
			if err := store.Save(key, paste, 0); err != nil {
				t.Errorf("concurrent save failed: %v", err)
			}
			done <- true
		}(i)
	}
	
	// Wait for all goroutines
	for i := 0; i < n; i++ {
		<-done
	}
}

func TestPostgresCleanupLoop(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	// Create an expired paste
	key := "test-pg-cleanup"
	paste := &PasteData{Content: []byte("expired")}
	if err := store.Save(key, paste, 1*time.Second); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer store.Delete(key)
	
	// Wait for expiry
	time.Sleep(2 * time.Second)
	
	// Trigger cleanup manually (normally runs every hour)
	pgStore := store.(*PostgresStorage)
	_, err := pgStore.db.Exec(`DELETE FROM pastes WHERE expire_at IS NOT NULL AND expire_at < NOW()`)
	if err != nil {
		t.Fatalf("manual cleanup failed: %v", err)
	}
	
	// Verify it's gone
	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get after cleanup: %v", err)
	}
	if got != nil {
		t.Fatal("paste should be deleted by cleanup")
	}
}

func TestPostgresTransactionIsolation(t *testing.T) {
	store, cleanup := NewTestPostgresStorage(t)
	defer cleanup()

	// Test that overlapping operations maintain consistency
	key := "test-pg-tx"
	paste := &PasteData{Content: []byte("initial")}
	if err := store.Save(key, paste, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	defer store.Delete(key)
	
	// Simulate concurrent update attempts
	const n = 5
	done := make(chan bool, n)
	
	for i := 0; i < n; i++ {
		go func() {
			// Try to save with same key - should get conflict
			newPaste := &PasteData{Content: []byte("updated")}
			err := store.Save(key, newPaste, 0)
			if err != ErrSlugConflict {
				t.Errorf("expected conflict error, got %v", err)
			}
			done <- true
		}()
	}
	
	for i := 0; i < n; i++ {
		<-done
	}
	
	// Original content should remain
	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got.Content, []byte("initial")) {
		t.Fatalf("content was corrupted: got %q, want initial", got.Content)
	}
}

// Run setup for PostgreSQL tests if needed
func TestMain(m *testing.M) {
	// Set up PostgreSQL test database if POSTGRES_TEST_DSN is set
	if dsn := os.Getenv("POSTGRES_TEST_DSN"); dsn != "" {
		// Extract the database name from DSN or use default
		// This is a simple setup - you might want to make it more robust
		db, err := sql.Open("postgres", dsn)
		if err == nil {
			defer db.Close()
			// Ensure the pastes table exists
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
				// Log but don't fail - tests will skip if connection fails
				println("Warning: Could not ensure pastes table:", err.Error())
			}
		}
	}
	
	// Run tests
	code := m.Run()
	os.Exit(code)
}