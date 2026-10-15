package ops

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// TestPM2DeployE2E is a real end-to-end check of the pm2 runtime: it creates a
// tiny node app in a local git repo, deploys it through RunCustom (clone -> npm
// install -> build-skip -> ecosystem -> pm2 start -> health check), and asserts
// the app answers on its PORT. It only runs when node/npm/pm2 are on PATH; set
// RUN_PM2_E2E=1 to require it (otherwise it's skipped when the toolchain or
// network for npm is unavailable).
func TestPM2DeployE2E(t *testing.T) {
	if os.Getenv("RUN_PM2_E2E") == "" {
		t.Skip("set RUN_PM2_E2E=1 to run the pm2 end-to-end test")
	}
	for _, bin := range []string{"git", "node", "npm", "pm2"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH: %v", bin, err)
		}
	}

	// 1. Build a tiny node app repo that honours PORT and serves 200 on /healthz.
	src := t.TempDir()
	writeTestFile(t, filepath.Join(src, "server.js"), `
const http = require("http");
const port = process.env.PORT || 3000;
http.createServer((req, res) => {
  if (req.url === "/healthz") { res.writeHead(200); res.end("ok"); return; }
  res.writeHead(200); res.end("hello");
}).listen(port, "127.0.0.1", () => console.log("listening on " + port));
`)
	writeTestFile(t, filepath.Join(src, "package.json"), `{
  "name": "pm2-e2e", "version": "1.0.0",
  "scripts": { "start": "node server.js" }
}`)
	gitInit(t, src)

	// 2. Deploy it as a pm2 web app via the real op.
	name := "pm2-e2e-test"
	port := 38765
	websites := t.TempDir()
	run := runner.New(runner.Options{PathEnv: os.Getenv("PATH"), Home: os.Getenv("HOME")})
	env := Env{Runner: run, WebsitesRoot: websites}

	params := jobs.DeployWebAppParams{
		Name:         name,
		Runtime:      jobs.WebRuntimePM2,
		URL:          "file://" + src,
		Branch:       "main",
		Commit:       gitHeadOf(t, src),
		InternalPort: port,
		HealthPath:   "/healthz",
		BuildCommand: "-", // nothing to build for this app
	}
	raw, _ := json.Marshal(params)
	job := protocol.Job{ID: 1, Type: jobs.TypeDeployWebApp, Params: raw}

	t.Cleanup(func() { _ = exec.Command("pm2", "delete", name).Run() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var logs []string
	res := RunCustom(ctx, env, job, func(stream, line string) { logs = append(logs, stream+": "+line) })

	if res.Status != "succeeded" {
		t.Fatalf("pm2 deploy failed: %s\n--- logs ---\n%s", res.Error, join(logs))
	}
	// pm2 should now list the app as online.
	out, _ := exec.Command("pm2", "jlist").Output()
	if !contains([]string{string(out)}, name) && !containsSub(string(out), name) {
		t.Fatalf("app %q not found in pm2 jlist", name)
	}
	t.Logf("pm2 deploy ok; %d log lines", len(logs))
}

// TestPM2ToolchainPrecheck verifies a pm2 deploy fails fast with a clear message
// when the toolchain is absent — the runner's PATH has neither npm nor pm2.
func TestPM2ToolchainPrecheck(t *testing.T) {
	run := runner.New(runner.Options{PathEnv: "/nonexistent-bin-dir"})
	env := Env{Runner: run, WebsitesRoot: t.TempDir()}
	params := jobs.DeployWebAppParams{
		Name: "x", Runtime: jobs.WebRuntimePM2, URL: "file:///tmp/none",
		Branch: "main", Commit: "0000000000000000000000000000000000000000", InternalPort: 39999,
	}
	raw, _ := json.Marshal(params)
	job := protocol.Job{ID: 1, Type: jobs.TypeDeployWebApp, Params: raw}
	res := RunCustom(context.Background(), env, job, func(string, string) {})
	if res.Status != "failed" {
		t.Fatalf("expected failure when pm2/npm missing, got %s", res.Status)
	}
	if !containsSub(res.Error, "pm2 runtime needs") {
		t.Fatalf("expected a toolchain-missing error, got %q", res.Error)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func gitHeadOf(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:40])
}

func join(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s + "\n"
	}
	return out
}

func containsSub(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
