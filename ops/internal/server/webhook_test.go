package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidSignature(t *testing.T) {
	secret := []byte("s3cr3t")
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !validSignature(secret, body, good) {
		t.Fatal("valid signature rejected")
	}
	if validSignature(secret, body, "sha256=deadbeef") {
		t.Fatal("bad signature accepted")
	}
	if validSignature(secret, body, "") {
		t.Fatal("empty signature accepted")
	}
	if validSignature(secret, body, "sha1=abc") {
		t.Fatal("wrong algo prefix accepted")
	}
	if validSignature([]byte("wrong"), body, good) {
		t.Fatal("signature valid under the wrong secret")
	}
}
