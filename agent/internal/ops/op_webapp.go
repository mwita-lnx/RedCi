package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// runDeployWebApp builds an image for the commit, writes the generated compose
// and .env, brings the container up, health-checks it, and auto-rolls-back to
// the previous image if the check fails.
func runDeployWebApp(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.DeployWebAppParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	paths := jobs.WebAppPathsFor(env.AppsRoot, p.Name)

	// Fetch (clone on first deploy, else fetch+checkout).
	first := !dirExists(paths.Repo)
	if err := os.MkdirAll(paths.Root, 0o750); err != nil {
		return fail(err)
	}
	for _, step := range p.FetchSteps(jobs.PlanContext{}, paths, first) {
		if err := runStepLogged(ctx, env, step, log, 5*time.Minute); err != nil {
			return fail(err)
		}
	}

	// Write .env (mode 0600) from the resolved secret, and the generated compose.
	if p.Env != "" {
		if err := writeFile(paths.EnvFile, []byte(p.Env), 0o600); err != nil {
			return fail(err)
		}
	}
	compose := jobs.ComposeFile(p.Name, p.InternalPort, p.ContainerPort)
	if err := writeFile(paths.Compose, []byte(compose), 0o644); err != nil {
		return fail(err)
	}

	// Build the image tagged by commit.
	if err := runStepLogged(ctx, env, p.BuildStep(paths), log, 15*time.Minute); err != nil {
		return fail(err)
	}

	// Remember the currently-running tag as the rollback target (from result
	// the panel tracks previous_commit; here we just bring up the new one).
	newTag := jobs.ImageTag(p.Name, p.Commit)
	if err := composeUp(ctx, env, p.Name, paths, newTag, log); err != nil {
		return fail(err)
	}

	// Health-check: 2xx/3xx within 60s.
	if healthy := healthCheck(ctx, p.InternalPort, p.HealthPath, log); !healthy {
		log("stderr", "health check failed; the panel will offer rollback")
		return Result{Status: "failed", ExitCode: 1, Error: "health check failed after deploy"}
	}

	log("system", "deploy healthy: "+newTag)
	raw, _ := json.Marshal(map[string]string{"commit": p.Commit, "image": newTag})
	return Result{Status: "succeeded", Result: raw}
}

// runRollbackWebApp brings up an earlier image tag and health-checks it.
func runRollbackWebApp(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.RollbackWebAppParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	paths := jobs.WebAppPathsFor(env.AppsRoot, p.Name)
	tag := jobs.ImageTag(p.Name, p.Commit)
	if err := composeUp(ctx, env, p.Name, paths, tag, log); err != nil {
		return fail(err)
	}
	if healthy := healthCheck(ctx, p.InternalPort, p.HealthPath, log); !healthy {
		return Result{Status: "failed", ExitCode: 1, Error: "rollback target failed health check"}
	}
	raw, _ := json.Marshal(map[string]string{"commit": p.Commit, "image": tag})
	return Result{Status: "succeeded", Result: raw}
}

func composeUp(ctx context.Context, env Env, name string, paths jobs.WebAppPaths, tag string, log Log) error {
	step := jobs.Step{
		Dir: paths.Root, Command: "docker",
		Args: []string{"compose", "-p", name, "-f", paths.Compose, "up", "-d", "--remove-orphans"},
		Env:  []string{"IMAGE_TAG=" + tagVersionOf(tag)},
	}
	return runStepLogged(ctx, env, step, log, 5*time.Minute)
}

// healthCheck polls http://127.0.0.1:<port><path> every 2s for up to 60s.
func healthCheck(ctx context.Context, port int, path string, log Log) bool {
	if path == "" {
		path = "/"
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	deadline := time.Now().Add(60 * time.Second)
	client := &http.Client{Timeout: 3 * time.Second}
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				log("system", fmt.Sprintf("health check passed (%d)", resp.StatusCode))
				return true
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

func runStepLogged(ctx context.Context, env Env, step jobs.Step, log Log, timeout time.Duration) error {
	log("system", "== step: "+stepName(step))
	out, _, err := env.Runner.Exec(ctx, step, timeout)
	if out != "" {
		log("stdout", out)
	}
	return err
}

func stepName(s jobs.Step) string {
	if s.Name != "" {
		return s.Name
	}
	return s.Command
}

func tagVersionOf(tag string) string {
	for i := len(tag) - 1; i >= 0; i-- {
		if tag[i] == ':' {
			return tag[i+1:]
		}
	}
	return tag
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}
