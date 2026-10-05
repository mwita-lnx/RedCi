package jobs

import (
	"strings"
	"testing"
)

func TestDeployFrappeAppSteps(t *testing.T) {
	p := DeployFrappeAppParams{
		BenchPath: "/home/frappe/frappe-bench", App: "erpnext", Branch: "main",
		Commit: "0123456789abcdef0123456789abcdef01234567",
		Sites:  []string{"a.example.com", "b.example.com"},
		HasPackage: true, Maintenance: true, CloneTok: "secret:clone:1",
	}
	if err := p.Validate([]string{"/home/frappe/frappe-bench"}); err != nil {
		t.Fatal(err)
	}
	steps, err := p.Steps(PlanContext{Secrets: map[SecretRef]string{"secret:clone:1": "TOK"}})
	if err != nil {
		t.Fatal(err)
	}
	// 2 backups + fetch + checkout + pip + yarn + 2 maint-on + 2 migrate + build + restart + 2 maint-off = 14
	if len(steps) != 14 {
		t.Fatalf("got %d steps, want 14", len(steps))
	}
	// Maintenance-off steps must be AlwaysRun (deferred cleanup).
	var offCount int
	for _, s := range steps {
		if strings.HasPrefix(s.Name, "maintenance off") {
			offCount++
			if !s.AlwaysRun {
				t.Errorf("step %q should be AlwaysRun", s.Name)
			}
		}
		// Token must never be in argv.
		for _, a := range s.Args {
			if strings.Contains(a, "TOK") {
				t.Errorf("clone token leaked into argv of %q", s.Name)
			}
		}
	}
	if offCount != 2 {
		t.Fatalf("expected 2 maintenance-off steps, got %d", offCount)
	}
	// Fetch step carries the token via GIT_CONFIG env (base64-wrapped), never
	// as plaintext in argv or env.
	var sawGitConfig bool
	for _, s := range steps {
		for _, e := range s.Env {
			if strings.HasPrefix(e, "GIT_CONFIG_COUNT=") {
				sawGitConfig = true
			}
			if strings.Contains(e, "TOK") {
				t.Errorf("plaintext token leaked into env: %q", e)
			}
		}
	}
	if !sawGitConfig {
		t.Error("expected GIT_CONFIG env on the fetch step")
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
