package main

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func newRedisStorageForTest(t *testing.T) *RedisStorage {
	t.Helper()
	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(func() {
		srv.Close()
	})

	s, err := newRedisStorage("redis://" + srv.Addr())
	if err != nil {
		t.Fatalf("newRedisStorage: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
	})
	return s
}

func TestNewRedisStorageConnects(t *testing.T) {
	s := newRedisStorageForTest(t)
	if s == nil {
		t.Fatal("expected non-nil RedisStorage")
	}
}

func TestRedisStorageSaveGetDeleteGetAndDelete(t *testing.T) {
	s := newRedisStorageForTest(t)
	paste := &PasteData{Content: []byte("value"), Burn: true, Encrypted: true, E2EEncrypted: true, Protected: true, Lang: "go"}
	if err := s.Save("key1", paste, 0); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := s.Get("key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil || got.Lang != "go" || !got.Burn || !got.Encrypted || !got.E2EEncrypted || !got.Protected {
		t.Fatalf("unexpected paste: %+v", got)
	}
	if err := s.Delete("key1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	got, err = s.Get("key1")
	if err != nil {
		t.Fatalf("Get after delete failed: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
	if err := s.Save("key2", &PasteData{Content: []byte("x")}, 0); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	burned, err := s.GetAndDelete("key2")
	if err != nil {
		t.Fatalf("GetAndDelete failed: %v", err)
	}
	if burned == nil || string(burned.Content) != "x" {
		t.Fatalf("unexpected burned paste: %+v", burned)
	}
}

func TestRedisStoragePeekMetaAndTTL(t *testing.T) {
	s := newRedisStorageForTest(t)
	if err := s.Save("meta-key", &PasteData{Content: []byte("meta"), Lang: "txt"}, 1*time.Second); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	meta, err := s.PeekMeta("meta-key")
	if err != nil {
		t.Fatalf("PeekMeta failed: %v", err)
	}
	if meta == nil || meta.Lang != "txt" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if meta.ExpireAt == nil {
		t.Fatal("expected ExpireAt to be set")
	}
}

func TestRedisStorageStatsReturnsPlaceholder(t *testing.T) {
	s := newRedisStorageForTest(t)
	st := s.Stats()
	if st.Backend != "redis" {
		t.Fatalf("expected redis backend stats, got %q", st.Backend)
	}
}
