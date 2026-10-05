// Package secrets seals and opens panel secrets with AES-256-GCM. Every
// ciphertext starts with a 1-byte key version so the master key can be
// rotated later without re-reading old data blindly.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KeyVersion1 is the only key version shipped today.
const KeyVersion1 byte = 1

// ErrWrongKey is returned when a ciphertext cannot be opened with the loaded key.
var ErrWrongKey = errors.New("secrets: cannot decrypt (wrong key or tampered ciphertext)")

// Sealer seals and opens secrets with one 32-byte master key.
type Sealer struct {
	gcm     cipher.AEAD
	version byte
}

// NewSealer builds a Sealer from a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{gcm: gcm, version: KeyVersion1}, nil
}

// Seal encrypts plaintext. Layout: [version byte][12-byte nonce][ciphertext+tag].
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 1, 1+len(nonce)+len(plaintext)+s.gcm.Overhead())
	out[0] = s.version
	out = append(out, nonce...)
	return s.gcm.Seal(out, nonce, plaintext, nil), nil
}

// Open reverses Seal.
func (s *Sealer) Open(blob []byte) ([]byte, error) {
	ns := s.gcm.NonceSize()
	if len(blob) < 1+ns {
		return nil, ErrWrongKey
	}
	if blob[0] != s.version {
		return nil, fmt.Errorf("secrets: unknown key version %d", blob[0])
	}
	nonce := blob[1 : 1+ns]
	plaintext, err := s.gcm.Open(nil, nonce, blob[1+ns:], nil)
	if err != nil {
		return nil, ErrWrongKey
	}
	return plaintext, nil
}

// LoadMasterKey reads the 32-byte master key. It prefers systemd's
// LoadCredential directory ($CREDENTIALS_DIRECTORY/master.key); if that is
// not set it falls back to the explicit path, which is handy in development.
func LoadMasterKey(fallbackPath string) ([]byte, error) {
	path := fallbackPath
	if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		path = filepath.Join(dir, "master.key")
	}
	if path == "" {
		return nil, errors.New("secrets: no master key path (set CREDENTIALS_DIRECTORY or OPS_MASTER_KEY)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read master key: %w", err)
	}
	key, err := normalizeKey(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: %s: %w", path, err)
	}
	return key, nil
}
