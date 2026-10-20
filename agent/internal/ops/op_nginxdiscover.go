package ops

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DiscoveredNginxVhost is an nginx vhost found in the standard nginx config
// directories (sites-enabled, conf.d) that RedCi does NOT manage — it was
// written by hand or by `bench setup nginx`. It is reported verbatim so the
// panel can show it read-only; RedCi never re-renders or overwrites it. This is
// the file-only, read-only counterpart to ops.d config management.
type DiscoveredNginxVhost struct {
	Path         string   `json:"path"`           // absolute path on the server
	ServerNames  []string `json:"server_names"`   // all server_name tokens across the file
	Content      string   `json:"content"`        // the verbatim file contents
	SHA256       string   `json:"sha256"`
	SSLExpiresAt int64    `json:"ssl_expires_at"` // cert notAfter (unix secs); 0 if none/unreadable
}

// nginxDiscoverDirs are the standard directories scanned for existing vhosts.
// RedCi's own configs live in ops.d (nginx.Dir) and are deliberately excluded
// so discovered (unmanaged) routes never collide with managed ones.
var nginxDiscoverDirs = []string{
	"/etc/nginx/sites-enabled",
	"/etc/nginx/conf.d",
}

// serverNameRe matches an nginx `server_name a b c;` directive, possibly spread
// across lines. We capture the token list and split on whitespace afterwards.
var serverNameRe = regexp.MustCompile(`(?s)\bserver_name\s+([^;]+);`)

// serverBlockRe matches the start of an nginx `server {` block, so files that
// hold only directives (log_format, map, upstream-only) are not mistaken for
// vhosts.
var serverBlockRe = regexp.MustCompile(`(?m)^\s*server\s*\{`)

// sslCertRe captures the path in an nginx `ssl_certificate <path>;` directive
// (the first one wins — that is the leaf/fullchain for the vhost).
var sslCertRe = regexp.MustCompile(`(?m)^\s*ssl_certificate\s+([^;]+);`)

// DiscoverNginxVhosts walks the standard nginx directories and reports every
// .conf vhost found (excluding RedCi's own ops.d and obvious backups). File-only
// and read-only, so it is safe in the periodic discovery push.
func DiscoverNginxVhosts() []DiscoveredNginxVhost {
	var out []DiscoveredNginxVhost
	seen := map[string]bool{}
	for _, dir := range nginxDiscoverDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".conf" {
				continue // skip .bak and non-.conf files
			}
			p := filepath.Join(dir, e.Name())
			if seen[p] {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if !serverBlockRe.Match(b) {
				continue // not a vhost (e.g. a bare log_format / map include)
			}
			seen[p] = true
			sum := sha256.Sum256(b)
			names := parseServerNames(string(b))
			out = append(out, DiscoveredNginxVhost{
				Path:         p,
				ServerNames:  names,
				Content:      string(b),
				SHA256:       hex.EncodeToString(sum[:]),
				SSLExpiresAt: certExpiry(string(b), names),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// certExpiry returns the vhost's leaf certificate notAfter (unix secs), or 0 if
// none/unreadable. It tries two sources, in order:
//  1. A localhost TLS handshake using the vhost's first server_name as SNI —
//     this reflects the actually-served cert and needs no filesystem access
//     (the agent runs unprivileged and /etc/letsencrypt/live is root-only).
//  2. Reading the ssl_certificate PEM off disk (works only if the agent can
//     read it, e.g. self-managed certs outside letsencrypt).
func certExpiry(content string, serverNames []string) int64 {
	for _, name := range serverNames {
		if ts := certNotAfterTLS(name); ts > 0 {
			return ts
		}
	}
	m := sslCertRe.FindStringSubmatch(content)
	if m == nil {
		return 0
	}
	path := strings.TrimSpace(m[1])
	if i := strings.Index(path, "#"); i >= 0 { // strip "# managed by Certbot"
		path = strings.TrimSpace(path[:i])
	}
	return certNotAfter(path)
}

// certNotAfterTLS opens a TLS connection to 127.0.0.1:443 with the given SNI and
// returns the served leaf cert's notAfter. Verification is skipped (we only read
// the expiry, not trust it) and it never follows the cert anywhere off-box.
func certNotAfterTLS(serverName string) int64 {
	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", "127.0.0.1:443", &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, // reading expiry only, not establishing trust
	})
	if err != nil {
		return 0
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return 0
	}
	return certs[0].NotAfter.Unix()
}

// certNotAfter reads a PEM cert bundle and returns the first (leaf) certificate's
// notAfter as a unix timestamp, or 0 on any error.
func certNotAfter(path string) int64 {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for block, rest := pem.Decode(pemBytes); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return 0
		}
		return cert.NotAfter.Unix() // first cert in fullchain is the leaf
	}
	return 0
}

// parseServerNames extracts the distinct, non-wildcard server_name tokens from a
// vhost file, skipping "_" and "$"-variable tokens (e.g. map-driven names).
func parseServerNames(content string) []string {
	var names []string
	seen := map[string]bool{}
	for _, m := range serverNameRe.FindAllStringSubmatch(content, -1) {
		for _, tok := range strings.Fields(m[1]) {
			tok = strings.TrimSpace(tok)
			if tok == "" || tok == "_" || strings.HasPrefix(tok, "$") || seen[tok] {
				continue
			}
			seen[tok] = true
			names = append(names, tok)
		}
	}
	return names
}
