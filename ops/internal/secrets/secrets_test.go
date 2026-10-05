package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, err := NewSealer(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range [][]byte{[]byte(""), []byte("hunter2"), bytes.Repeat([]byte("x"), 4096)} {
		blob, err := s.Seal(pt)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		if blob[0] != KeyVersion1 {
			t.Fatalf("version byte = %d, want %d", blob[0], KeyVersion1)
		}
		got, err := s.Open(blob)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("round trip mismatch")
		}
	}
}

func TestOpenWrongKey(t *testing.T) {
	a, _ := NewSealer(newKey(t))
	b, _ := NewSealer(newKey(t))
	blob, _ := a.Seal([]byte("secret"))
	if _, err := b.Open(blob); err != ErrWrongKey {
		t.Fatalf("want ErrWrongKey, got %v", err)
	}
}

func TestOpenTampered(t *testing.T) {
	s, _ := NewSealer(newKey(t))
	blob, _ := s.Seal([]byte("secret"))
	blob[len(blob)-1] ^= 0xFF // flip a tag bit
	if _, err := s.Open(blob); err != ErrWrongKey {
		t.Fatalf("want ErrWrongKey on tamper, got %v", err)
	}
}

func TestNewSealerBadKeyLen(t *testing.T) {
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestNormalizeKey(t *testing.T) {
	// A deterministic 32-byte key, including leading/trailing whitespace bytes
	// to prove a raw binary key is NOT trimmed.
	want := make([]byte, 32)
	for i := range want {
		want[i] = byte(i)
	}
	want[0] = ' '
	want[31] = '\n'

	cases := map[string][]byte{
		"raw-32-bytes":  want,
		"hex-64-chars":  []byte(hex.EncodeToString(want)),
		"hex-with-nl":   []byte(hex.EncodeToString(want) + "\n"),
		"base64-std":    []byte(base64.StdEncoding.EncodeToString(want)),
	}
	for name, in := range cases {
		got, err := normalizeKey(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: mismatch\n got=%x\nwant=%x", name, got, want)
		}
	}

	// Wrong length must be rejected.
	if _, err := normalizeKey([]byte("too short")); err == nil {
		t.Fatal("short key should be rejected")
	}
}
