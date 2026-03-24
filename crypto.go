package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Crypto wraps AES-256-GCM encryption.
// The key must be exactly 32 bytes (AES-256).
// Ciphertext format: base64( nonce(12) || ciphertext || tag(16) )
type Crypto struct {
	key []byte
}

func newCrypto(b64Key string) (*Crypto, error) {
	if b64Key == "" {
		return nil, errors.New("PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY is empty; set a 32-byte base64 key")
	}
	key, err := base64.StdEncoding.DecodeString(b64Key)
	if err != nil {
		// Try raw bytes if not base64
		key = []byte(b64Key)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("Encryption key must be 32 bytes, got %d", len(key))
	}
	return &Crypto{key: key}, nil
}

func (c *Crypto) encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Seal appends ciphertext+tag to nonce
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Crypto) decrypt(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("Base64 decode failed: %w", err)
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
