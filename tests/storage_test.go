package main_test

import (
    "testing"
	"github.com/GAS85/ownPastebin"
)

func TestStorageSaveGetDelete(t *testing.T) {
    app := newTestApp(t)

    key := "test123"
    app.Storage.Save(key, map[string]string{"content": "abc"}, 0)

    data := app.Storage.Get(key)
    if data["content"] != "abc" {
        t.Fatalf("expected 'abc', got %v", data["content"])
    }

    app.Storage.Delete(key)
    if app.Storage.Get(key) != nil {
        t.Fatalf("expected nil after delete")
    }
}

func TestStorageTTL(t *testing.T) {
    app := newTestApp(t)

    key := "ttl_test"
    app.Storage.Save(key, map[string]string{"content": "temp"}, 1) // ttl 1 second

    time.Sleep(2 * time.Second)
    if app.Storage.Get(key) != nil {
        t.Fatalf("expected expired paste to be nil")
    }
}