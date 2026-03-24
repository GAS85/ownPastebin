package main

import (
	"os"
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

	s, err := newSQLiteStorage(path)
	if err != nil {
		t.Fatalf("newSQLiteStorage: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		os.Remove(path)
	})
	return s
}

func TestStorageSaveAndGet(t *testing.T) {
	s := newTestStorage(t)

	paste := &PasteData{Content: "aGVsbG8=", Lang: "text"}
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
	if got.Content != "aGVsbG8=" {
		t.Fatalf("content mismatch: got %q", got.Content)
	}
}

func TestStorageDelete(t *testing.T) {
	s := newTestStorage(t)

	s.Save("key2", &PasteData{Content: "x"}, 0)
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

	s.Save("ttl1", &PasteData{Content: "temp"}, 1*time.Second)

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

	s.Save("burn1", &PasteData{Content: "burnme", Burn: true}, 0)

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

	s.Save("exp1", &PasteData{Content: "data"}, 1*time.Hour)

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

	s.Save("perm1", &PasteData{Content: "permanent"}, 0)

	got, err := s.Get("perm1")
	if err != nil || got == nil {
		t.Fatalf("Get: err=%v got=%v", err, got)
	}
	if got.ExpireAt != nil {
		t.Fatalf("ExpireAt should be nil for permanent paste, got %v", got.ExpireAt)
	}
}
