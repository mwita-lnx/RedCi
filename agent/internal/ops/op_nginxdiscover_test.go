package ops

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParseServerNames covers the server_name extraction that drives a
// discovered route's display domain, including the multi-name and map-driven
// ($variable) shapes seen in real Frappe vhosts.
func TestParseServerNames(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "server {\n server_name dt.redcross.or.ke;\n}", []string{"dt.redcross.or.ke"}},
		{"multi across lines", "server_name\n\tone.dev.redcross.or.ke\n\ttrainers.dev.krcti.ac.ke\n\tone.dev.redcross.or.ke\n\t;",
			[]string{"one.dev.redcross.or.ke", "trainers.dev.krcti.ac.ke"}},
		{"skip variable + underscore", "server_name $site_name_x _;", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseServerNames(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

// TestDiscoverNginxVhostsFromDir verifies the scanner reads .conf files
// verbatim, skips .bak/non-.conf, and records sha256 + server_names. It points
// the scanner at a temp dir by temporarily overriding nginxDiscoverDirs.
func TestDiscoverNginxVhostsFromDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "dt-frappe.conf"),
		"server {\n  server_name dt.redcross.or.ke;\n  ssl_certificate /x/fullchain.pem;\n}")
	mustWrite(t, filepath.Join(dir, "plain.conf"),
		"server {\n  server_name plain.example.com;\n}")
	mustWrite(t, filepath.Join(dir, "dt-frappe.conf.bak"), "server { server_name old; }")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "ignore me")
	// a directive-only .conf (no server {} block) must be skipped
	mustWrite(t, filepath.Join(dir, "00-log-format.conf"), "log_format main '$remote_addr';")

	old := nginxDiscoverDirs
	nginxDiscoverDirs = []string{dir}
	defer func() { nginxDiscoverDirs = old }()

	got := DiscoverNginxVhosts()
	if len(got) != 2 {
		t.Fatalf("expected 2 vhosts (.bak and .txt skipped), got %d: %+v", len(got), got)
	}
	// Sorted by path: dt-frappe.conf before plain.conf
	if got[0].ServerNames[0] != "dt.redcross.or.ke" {
		t.Errorf("first vhost server_name = %v", got[0].ServerNames)
	}
	if got[0].SHA256 == "" || got[0].Content == "" {
		t.Error("expected content + sha256 to be populated")
	}
}

// TestCertExpiryFromVhost writes a self-signed cert, points a vhost's
// ssl_certificate at it, and checks the parsed notAfter matches.
func TestCertExpiryFromVhost(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "fullchain.pem")
	want := time.Now().Add(42 * 24 * time.Hour).Truncate(time.Second)
	writeSelfSigned(t, certPath, want)

	vhost := "server {\n  server_name x.example.com;\n  ssl_certificate " + certPath + "; # managed by Certbot\n}"
	// Pass no server names so it skips the TLS dial and exercises the file path.
	got := certExpiry(vhost, nil)
	if got != want.Unix() {
		t.Fatalf("expiry = %d, want %d", got, want.Unix())
	}

	// No cert directive → 0
	if certExpiry("server { server_name y; }", nil) != 0 {
		t.Error("expected 0 for a vhost with no ssl_certificate")
	}
}

func writeSelfSigned(t *testing.T, path string, notAfter time.Time) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    notAfter.Add(-24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
}
