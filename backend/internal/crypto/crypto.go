// Package crypto provides AES-256-GCM encryption for secrets stored in the DB.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

type Box struct{ aead cipher.AEAD }

// New takes a 64-char hex string (32 bytes). Generate: openssl rand -hex 32
func New(hexKey string) (*Box, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("ENCRYPTION_KEY must be 64 hex characters (32 bytes)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt returns nonce || ciphertext.
func (b *Box) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

func (b *Box) Decrypt(data []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(data) < ns+b.aead.Overhead() {
		return nil, errors.New("ciphertext too short")
	}
	return b.aead.Open(nil, data[:ns], data[ns:], nil)
}
