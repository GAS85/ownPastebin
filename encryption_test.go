package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// testEncKey is a valid base64-encoded 32-byte key for use in encryption tests.
var testEncKey = base64.StdEncoding.EncodeToString([]byte("pastebin-test-key-32bytes!!!!!!!"))

func TestEncryptionRoundtrip(t *testing.T) {
	_, handler := NewAppForTest(t, TestConfig{
		EncryptionEnabled: true,
		EncryptionKey:     testEncKey,
	})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("secret content"))
	if res.Code != 201 {
		t.Fatalf("create: expected 201, got %d", res.Code)
	}
	id := extractID(t, res.Body.String())

	res = doRequest(t, handler, "GET", "/raw/"+id, nil)
	if res.Code != 200 {
		t.Fatalf("raw: expected 200, got %d", res.Code)
	}
	if res.Body.String() != "secret content" {
		t.Fatalf("encryption roundtrip failed: got %q", res.Body.String())
	}
}

func TestEncryptionStoredValueIsNotPlaintext(t *testing.T) {
	app, handler := NewAppForTest(t, TestConfig{
		EncryptionEnabled: true,
		EncryptionKey:     testEncKey,
	})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("topsecret"))
	id := extractID(t, res.Body.String())

	// Read the raw stored payload directly from storage — it must NOT be
	// the plaintext, it must be the AES-GCM ciphertext (base64).
	paste, err := app.storage.Get(id)
	if err != nil {
		t.Fatalf("storage.Get: %v", err)
	}
	if paste == nil {
		t.Fatal("paste not found in storage")
	}
	if paste.Content == "topsecret" {
		t.Fatal("plaintext found in storage — encryption is not working")
	}
	if !strings.Contains(paste.Content, "=") && len(paste.Content) < 32 {
		t.Fatal("stored content doesn't look like base64 ciphertext")
	}
}

func TestEncryptionWithBadKeyFails(t *testing.T) {
	// 31 bytes — one short of the required 32.
	badKey := base64.StdEncoding.EncodeToString([]byte("tooshort-31bytes!!!!!!!!!!!!!!!"))

	cfg := &Settings{
		ServerSideEncryptionEnabled: true,
		ServerSideEncryptionKey:     badKey,
	}
	_ = cfg // just make sure config is built; newCrypto is what we want to test

	_, err := newCrypto(badKey)
	if err == nil {
		t.Fatal("expected error for bad key length, got nil")
	}
}

func TestEncryptionDisabledStoresBase64(t *testing.T) {
	app, handler := NewAppForTest(t, TestConfig{
		EncryptionEnabled: false,
	})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("plaindata"))
	id := extractID(t, res.Body.String())

	paste, err := app.storage.Get(id)
	if err != nil || paste == nil {
		t.Fatalf("storage.Get: err=%v paste=%v", err, paste)
	}
	// Without encryption the content is plain base64 — decoding it must
	// produce the original bytes.
	if paste.Content == "plaindata" {
		t.Fatal("content stored as literal plaintext, expected base64")
	}
	// Raw endpoint must still return the original content transparently.
	res = doRequest(t, handler, "GET", "/raw/"+id, nil)
	if res.Body.String() != "plaindata" {
		t.Fatalf("raw: got %q, want %q", res.Body.String(), "plaindata")
	}
}
