package jobs

import (
	"strings"
	"testing"
)

// deploy_frappe_app and rollback_frappe_app run as agent custom ops now, so the
// step sequence is tested there. Here we cover the param validation and the
// shared clone-token env helper (the security-relevant invariant: the token
// rides in GIT_CONFIG, base64-wrapped, never as plaintext in argv or env).
func TestDeployFrappeAppValidate(t *testing.T) {
	p := DeployFrappeAppParams{
		BenchPath: "/home/frappe/frappe-bench", App: "erpnext", Branch: "main",
		Commit: "0123456789abcdef0123456789abcdef01234567",
		Sites:  []string{"a.example.com", "b.example.com"},
		HasPackage: true, Maintenance: true, CloneTok: "secret:clone:1",
	}
	if err := p.Validate([]string{"/home/frappe/frappe-bench"}); err != nil {
		t.Fatal(err)
	}
	// Rollback params validate on the same bench rules.
	r := RollbackFrappeAppParams{
		BenchPath: "/home/frappe/frappe-bench", App: "erpnext", Branch: "main",
		Commit: "0123456789abcdef0123456789abcdef01234567",
		Sites:  []string{"a.example.com"},
		Backups: map[string]string{"a.example.com": "/x/backups/db.sql.gz"},
	}
	if err := r.Validate([]string{"/home/frappe/frappe-bench"}); err != nil {
		t.Fatal(err)
	}
}

func TestGitTokenEnv(t *testing.T) {
	// No token: no env.
	if env := gitTokenEnv(""); env != nil {
		t.Fatalf("empty token should yield nil env, got %v", env)
	}
	env := gitTokenEnv("TOK")
	var sawCount bool
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_CONFIG_COUNT=") {
			sawCount = true
		}
		if strings.Contains(e, "TOK") {
			t.Errorf("plaintext token leaked into env: %q", e)
		}
	}
	if !sawCount {
		t.Error("expected GIT_CONFIG_COUNT in token env")
	}
}

func TestRenderRouteGolden(t *testing.T) {
	cases := []struct {
		name string
		data RouteData
		want []string // substrings that must appear
		deny []string // substrings that must NOT appear
	}{
		{
			name: "http only",
			data: RouteData{ID: 1, Domain: "a.example.com", Upstream: "http://127.0.0.1:3001"},
			want: []string{"listen 80;", "proxy_pass http://127.0.0.1:3001;", "route=1"},
			deny: []string{"listen 443", "ssl_certificate"},
		},
		{
			name: "ssl new nginx",
			data: RouteData{ID: 2, Domain: "b.example.com", Upstream: "http://127.0.0.1:3002", SSL: true, HTTP2On: true},
			want: []string{"return 301 https://$host$request_uri;", "http2 on;", "ssl_certificate     /etc/letsencrypt/live/b.example.com/fullchain.pem;"},
			deny: []string{"listen 443 ssl http2;"},
		},
		{
			name: "ssl old nginx",
			data: RouteData{ID: 3, Domain: "c.example.com", Upstream: "http://127.0.0.1:3003", SSL: true, HTTP2On: false},
			want: []string{"listen 443 ssl http2;"},
			deny: []string{"http2 on;"},
		},
		{
			name: "with snippet",
			data: RouteData{ID: 4, Domain: "d.example.com", Upstream: "http://127.0.0.1:3004", Snippet: "add_header X-Test 1;"},
			want: []string{"add_header X-Test 1;"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RenderRoute(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(out, d) {
					t.Errorf("unexpected %q in:\n%s", d, out)
				}
			}
		})
	}
}

func TestWebAppHelpers(t *testing.T) {
	paths := WebAppPathsFor("/srv/ops/apps", "shop")
	if paths.Repo != "/srv/ops/apps/shop/repo" || paths.Compose != "/srv/ops/apps/shop/compose.yaml" {
		t.Fatalf("unexpected paths: %+v", paths)
	}
	if got := ImageTag("shop", "0123456789abcdef0123456789abcdef01234567"); got != "ops/shop:0123456789ab" {
		t.Fatalf("ImageTag = %q", got)
	}
	compose := ComposeFile("shop", 3101, 3000)
	for _, w := range []string{`name: shop`, `127.0.0.1:3101:3000`, `image: ops/shop:${IMAGE_TAG}`, `restart: unless-stopped`} {
		if !strings.Contains(compose, w) {
			t.Errorf("compose missing %q:\n%s", w, compose)
		}
	}
}

func TestValidateRepoAndBranch(t *testing.T) {
	for _, r := range []string{"owner/name", "my-org/my.repo", "a1/b2"} {
		if err := validateRepo(r); err != nil {
			t.Errorf("repo %q should be valid: %v", r, err)
		}
	}
	for _, r := range []string{"", "noslash", "a/b/c", "a b/c", "owner/name; rm", "/x", "x/"} {
		if err := validateRepo(r); err == nil {
			t.Errorf("repo %q should be invalid", r)
		}
	}
	for _, b := range []string{"main", "release/1.0", "feature-x"} {
		if err := validateBranch(b); err != nil {
			t.Errorf("branch %q should be valid: %v", b, err)
		}
	}
	for _, b := range []string{"", "../evil", "a b", "x;y", "-start"} {
		if err := validateBranch(b); err == nil {
			t.Errorf("branch %q should be invalid", b)
		}
	}
}
