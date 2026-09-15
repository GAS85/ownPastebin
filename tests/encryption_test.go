package main

import (
	"bytes"
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
	// the plaintext. Content is []byte raw AES-GCM ciphertext (nonce || ct || tag).
	paste, err := app.storage.Get(id)
	if err != nil {
		t.Fatalf("storage.Get: %v", err)
	}
	if paste == nil {
		t.Fatal("paste not found in storage")
	}

	// Must not equal the plaintext bytes.
	if bytes.Equal(paste.Content, []byte("topsecret")) {
		t.Fatal("plaintext found in storage — encryption is not working")
	}

	// AES-GCM output is: 12-byte nonce + ciphertext + 16-byte tag.
	// Minimum size for any input is 12 + 0 + 16 = 28 bytes.
	if len(paste.Content) < 28 {
		t.Fatalf("stored content too short to be AES-GCM ciphertext: %d bytes", len(paste.Content))
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

func TestEncryptionDisabledStoresRawBytes(t *testing.T) {
	app, handler := NewAppForTest(t, TestConfig{
		EncryptionEnabled: false,
	})

	res := doRequest(t, handler, "POST", "/", strings.NewReader("plaindata"))
	id := extractID(t, res.Body.String())

	paste, err := app.storage.Get(id)
	if err != nil || paste == nil {
		t.Fatalf("storage.Get: err=%v paste=%v", err, paste)
	}

	// Without encryption the content is stored as raw bytes identical to the input.
	if !bytes.Equal(paste.Content, []byte("plaindata")) {
		t.Fatalf("expected raw bytes in storage, got %q", paste.Content)
	}

	// Raw endpoint must still return the original content transparently.
	res = doRequest(t, handler, "GET", "/raw/"+id, nil)
	if res.Body.String() != "plaindata" {
		t.Fatalf("raw: got %q, want %q", res.Body.String(), "plaindata")
	}
}
