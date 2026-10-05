package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// normalizeKey accepts a master key as 32 raw bytes, 64 hex chars, or
// base64 (std or url, with or without padding), and returns 32 raw bytes.
// Trailing whitespace/newlines from `echo ... > master.key` are trimmed.
func normalizeKey(raw []byte) ([]byte, error) {
	// Exactly 32 bytes is a raw binary key — use it verbatim, without trimming
	// (a random key may legitimately begin or end with a whitespace byte).
	if len(raw) == 32 {
		return raw, nil
	}
	// Otherwise treat it as text (hex/base64) and trim the trailing newline a
	// shell `echo ... > master.key` would add.
	raw = bytes.TrimSpace(raw)
	if len(raw) == 32 {
		return raw, nil
	}
	if len(raw) == 64 {
		if k, err := hex.DecodeString(string(raw)); err == nil && len(k) == 32 {
			return k, nil
		}
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if k, err := enc.DecodeString(string(raw)); err == nil && len(k) == 32 {
			return k, nil
		}
	}
	return nil, errors.New("master key must be 32 raw bytes, 64 hex chars, or base64 of 32 bytes")
}
