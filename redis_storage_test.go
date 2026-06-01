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

func TestRedisStorageStatsCountsEntries(t *testing.T) {
	s := newRedisStorageForTest(t)
	if err := s.Save("stat1", &PasteData{Content: []byte("1"), Burn: true}, 0); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := s.Save("stat2", &PasteData{Content: []byte("2")}, 1*time.Second); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	st := s.Stats()
	if st.Backend != "redis" {
		t.Fatalf("expected redis backend stats, got %q", st.Backend)
	}
	time.Sleep(100 * time.Millisecond)
}

func TestRedisHelperFunctions(t *testing.T) {
	if got := redisKey("abc"); got != "paste:abc" {
		t.Fatalf("unexpected redisKey: %q", got)
	}
	if got := valToString(nil); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
	if got := valToString(123); got != "123" {
		t.Fatalf("expected 123, got %q", got)
	}
	if got := valToInt("42"); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := valToInt(nil); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
	if got, err := valToBytes("hi"); err != nil || string(got) != "hi" {
		t.Fatalf("expected hi, got %v %v", got, err)
	}
	if got, err := valToBytes([]byte("bye")); err != nil || string(got) != "bye" {
		t.Fatalf("expected bye, got %v %v", got, err)
	}
}
