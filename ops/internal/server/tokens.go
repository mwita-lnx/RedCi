package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// newToken returns a URL-safe random token (32 bytes, hex) and its sha256 hash.
// The panel stores only the hash; the plaintext is shown/sent exactly once.
func newToken() (token, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = hex.EncodeToString(b)
	return token, hashToken(token)
}

// hashToken returns the hex sha256 of a token for constant-time storage lookups.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
