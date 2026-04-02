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
		// fallback: treat as raw string
		key = []byte(b64Key)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	return &Crypto{key: key}, nil
}

// Encrypt returns raw binary: nonce || ciphertext || tag
func (c *Crypto) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// prepend nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	return ciphertext, nil
}

// Decrypt expects raw binary: nonce || ciphertext || tag
func (c *Crypto) Decrypt(data []byte) ([]byte, error) {
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

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}