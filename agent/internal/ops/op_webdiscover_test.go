package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// TestDiscoverWebApps verifies the file-only scanner recognises both runtimes,
// parses the internal port out of the agent-generated runtime file, reads git
// state from the checked-out repo, and ignores unrelated directories.
func TestDiscoverWebApps(t *testing.T) {
	root := t.TempDir()

	// docker app, with a git repo checked out at a branch
	dockerApp := filepath.Join(root, "storefront")
	mustMkdir(t, filepath.Join(dockerApp, "repo", ".git", "refs", "heads"))
	mustWrite(t, filepath.Join(dockerApp, "compose.yaml"),
		jobs.ComposeFile("storefront", 3100, 3000))
	mustWrite(t, filepath.Join(dockerApp, "repo", ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(dockerApp, "repo", ".git", "refs", "heads", "main"), "abc123def456\n")
	mustWrite(t, filepath.Join(dockerApp, "repo", ".git", "config"),
		"[remote \"origin\"]\n\turl = git@github.com:acme/storefront.git\n")

	// pm2 app, no git repo
	pm2App := filepath.Join(root, "admin-ui")
	mustMkdir(t, pm2App)
	mustWrite(t, filepath.Join(pm2App, "ecosystem.config.js"),
		jobs.PM2EcosystemFile("admin-ui", filepath.Join(pm2App, "repo"), "npm start", 3200, ".env"))

	// unrelated directory — must be ignored
	mustMkdir(t, filepath.Join(root, "random-stuff"))
	mustWrite(t, filepath.Join(root, "random-stuff", "notes.txt"), "hello")

	got := DiscoverWebApps([]string{root})
	if len(got) != 2 {
		t.Fatalf("expected 2 web apps, got %d: %+v", len(got), got)
	}

	byName := map[string]DiscoveredWebApp{}
	for _, a := range got {
		byName[a.Name] = a
	}

	sf, ok := byName["storefront"]
	if !ok {
		t.Fatal("storefront not discovered")
	}
	if sf.Runtime != "docker" {
		t.Errorf("storefront runtime = %q, want docker", sf.Runtime)
	}
	if sf.InternalPort != 3100 {
		t.Errorf("storefront port = %d, want 3100", sf.InternalPort)
	}
	if sf.Git.Branch != "main" || sf.Git.Commit != "abc123def456" {
		t.Errorf("storefront git = %+v, want branch=main commit=abc123def456", sf.Git)
	}
	if sf.Git.RemoteURL != "acme/storefront" {
		t.Errorf("storefront remote = %q, want acme/storefront", sf.Git.RemoteURL)
	}

	ui, ok := byName["admin-ui"]
	if !ok {
		t.Fatal("admin-ui not discovered")
	}
	if ui.Runtime != "pm2" {
		t.Errorf("admin-ui runtime = %q, want pm2", ui.Runtime)
	}
	if ui.InternalPort != 3200 {
		t.Errorf("admin-ui port = %d, want 3200", ui.InternalPort)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
