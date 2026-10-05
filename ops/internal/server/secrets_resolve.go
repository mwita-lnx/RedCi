package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// resolveSecrets walks a params JSON object and replaces every string value
// that looks like a secret reference ("secret:<name>") with its plaintext,
// opened with the master key. Only top-level string fields are resolved, which
// is all the job params use. The result is sent to the agent over TLS only and
// is never stored.
func (s *Server) resolveSecrets(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, nil // not an object (e.g. {} params); nothing to resolve
	}
	changed := false
	for k, v := range obj {
		var str string
		if err := json.Unmarshal(v, &str); err != nil {
			continue // not a string field
		}
		if !strings.HasPrefix(str, "secret:") {
			continue
		}
		name := strings.TrimPrefix(str, "secret:")
		plain, err := s.openSecret(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", k, err)
		}
		enc, _ := json.Marshal(plain)
		obj[k] = enc
		changed = true
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(obj)
}

// openSecret loads and decrypts a stored secret by name.
func (s *Server) openSecret(ctx context.Context, name string) (string, error) {
	enc, err := s.db.ReadQ.GetSecret(ctx, name)
	if err != nil {
		return "", fmt.Errorf("secret %q not found: %w", name, err)
	}
	plain, err := s.sealer.Open(enc)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// putSecret seals and stores a secret by name.
func (s *Server) putSecret(ctx context.Context, name, value string) error {
	enc, err := s.sealer.Seal([]byte(value))
	if err != nil {
		return err
	}
	return s.db.WriteQ.PutSecret(ctx, store.PutSecretParams{Name: name, ValueEnc: enc})
}
