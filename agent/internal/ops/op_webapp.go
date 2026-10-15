package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// webAppRoot returns the websites root the app lives under: the per-job override
// (p.WebsitesRoot) when set, else the agent's configured root, else AppsRoot for
// backward compatibility.
func webAppRoot(env Env, override string) string {
	if override != "" {
		return override
	}
	if env.WebsitesRoot != "" {
		return env.WebsitesRoot
	}
	return env.AppsRoot
}

// runDeployWebApp fetches the repo, then deploys via the app's runtime: docker
// (build image + compose up) or pm2 (npm build + pm2 start). It health-checks
// the result; a failure leaves the panel able to offer rollback.
func runDeployWebApp(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.DeployWebAppParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	paths := jobs.WebAppPathsFor(webAppRoot(env, p.WebsitesRoot), p.Name)

	// For pm2, verify the toolchain BEFORE cloning so a host missing pm2/npm
	// fails instantly with guidance rather than after a pointless clone.
	if p.Runtime == jobs.WebRuntimePM2 {
		if res := requirePM2Toolchain(env, log); res != nil {
			return *res
		}
	}

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

	// Always write .env (mode 0600) from the resolved secret — compose/pm2
	// reference it via env_file, so the file must exist even when empty.
	if err := writeFile(paths.EnvFile, []byte(p.Env), 0o600); err != nil {
		return fail(err)
	}

	if p.Runtime == jobs.WebRuntimePM2 {
		return deployPM2(ctx, env, p, paths, log)
	}
	return deployDocker(ctx, env, p, paths, log)
}

// deployDocker builds the image, writes the generated compose, brings the
// container up and health-checks it.
func deployDocker(ctx context.Context, env Env, p jobs.DeployWebAppParams, paths jobs.WebAppPaths, log Log) Result {
	// Pre-check the Dockerfile exists so we fail with a clear message instead of
	// a raw `docker build` error after the clone.
	dfRel := p.DockerfileRel()
	if dfRel == "" {
		dfRel = "Dockerfile"
	}
	dfPath := filepath.Join(paths.Repo, dfRel)
	if _, err := os.Stat(dfPath); err != nil {
		log("stderr", "no Dockerfile at "+dfRel+" in the repo — set a Dockerfile path, add one, or use the pm2 runtime for a plain Node app")
		return Result{Status: "failed", ExitCode: 1, Error: "Dockerfile not found at " + dfRel}
	}
	compose := jobs.ComposeFile(p.Name, p.InternalPort, p.ContainerPort)
	if err := writeFile(paths.Compose, []byte(compose), 0o644); err != nil {
		return fail(err)
	}
	if err := runStepLogged(ctx, env, p.BuildStep(paths), log, 15*time.Minute); err != nil {
		return fail(err)
	}
	newTag := jobs.ImageTag(p.Name, p.Commit)
	if err := composeUp(ctx, env, p.Name, paths, newTag, log); err != nil {
		return fail(err)
	}
	if healthy := healthCheck(ctx, p.InternalPort, p.HealthPath, log); !healthy {
		log("stderr", "health check failed; the panel will offer rollback")
		return Result{Status: "failed", ExitCode: 1, Error: "health check failed after deploy"}
	}
	log("system", "deploy healthy: "+newTag)
	raw, _ := json.Marshal(map[string]string{"commit": p.Commit, "image": newTag})
	return Result{Status: "succeeded", Result: raw}
}

// requirePM2Toolchain verifies node/npm and pm2 are on the agent PATH before a
// pm2 deploy touches the host, so a missing tool fails with clear guidance
// instead of a raw "command not found" mid-build.
func requirePM2Toolchain(env Env, log Log) *Result {
	missing := []string{}
	for _, bin := range []string{"npm", "pm2"} {
		if !env.Runner.LookPath(bin) {
			missing = append(missing, bin)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	hint := "install it and ensure it is on the agent PATH (OPS_AGENT_BENCH_PATH_ENV)"
	if contains(missing, "pm2") {
		hint = "install pm2 (`npm i -g pm2`) and ensure node/npm/pm2 are on the agent PATH (OPS_AGENT_BENCH_PATH_ENV); or use the docker runtime"
	}
	msg := "pm2 runtime needs " + strings.Join(missing, ", ") + " — " + hint
	log("stderr", msg)
	r := Result{Status: "failed", ExitCode: 1, Error: msg}
	return &r
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// deployPM2 installs deps, builds, writes the ecosystem config, and (re)starts
// the app under pm2, then health-checks it.
func deployPM2(ctx context.Context, env Env, p jobs.DeployWebAppParams, paths jobs.WebAppPaths, log Log) Result {
	// Toolchain is pre-checked in runDeployWebApp before the clone.
	for _, step := range p.PM2BuildSteps(paths) {
		if err := runStepLogged(ctx, env, step, log, 15*time.Minute); err != nil {
			return fail(err)
		}
	}
	eco := jobs.PM2EcosystemFile(p.Name, paths.Repo, p.StartCommand, p.InternalPort, paths.EnvFile)
	if err := writeFile(paths.Ecosystem, []byte(eco), 0o644); err != nil {
		return fail(err)
	}
	if err := runStepLogged(ctx, env, p.PM2StartStep(paths), log, 5*time.Minute); err != nil {
		return fail(err)
	}
	if healthy := healthCheck(ctx, p.InternalPort, p.HealthPath, log); !healthy {
		log("stderr", "health check failed; the panel will offer rollback")
		return Result{Status: "failed", ExitCode: 1, Error: "health check failed after deploy"}
	}
	log("system", "deploy healthy (pm2): "+p.Name+" @ "+shortCommit(p.Commit))
	raw, _ := json.Marshal(map[string]string{"commit": p.Commit, "runtime": "pm2"})
	return Result{Status: "succeeded", Result: raw}
}

// runRollbackWebApp restores an earlier commit: for docker it brings up the
// earlier image tag; for pm2 it checks out the earlier commit, rebuilds and
// reloads. Both health-check the result.
func runRollbackWebApp(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.RollbackWebAppParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	paths := jobs.WebAppPathsFor(webAppRoot(env, p.WebsitesRoot), p.Name)

	if p.Runtime == jobs.WebRuntimePM2 {
		if res := requirePM2Toolchain(env, log); res != nil {
			return *res
		}
		// Check out the rollback commit, rebuild, reload.
		checkout := jobs.Step{Name: "checkout", Dir: paths.Repo, Command: "git",
			Args: []string{"checkout", "--force", p.Commit}}
		if err := runStepLogged(ctx, env, checkout, log, 5*time.Minute); err != nil {
			return fail(err)
		}
		rebuild := jobs.DeployWebAppParams{
			Name: p.Name, InternalPort: p.InternalPort, StartCommand: p.StartCommand, BuildCommand: p.BuildCommand,
		}
		for _, step := range rebuild.PM2BuildSteps(paths) {
			if err := runStepLogged(ctx, env, step, log, 15*time.Minute); err != nil {
				return fail(err)
			}
		}
		if err := runStepLogged(ctx, env, rebuild.PM2StartStep(paths), log, 5*time.Minute); err != nil {
			return fail(err)
		}
		if healthy := healthCheck(ctx, p.InternalPort, p.HealthPath, log); !healthy {
			return Result{Status: "failed", ExitCode: 1, Error: "rollback target failed health check"}
		}
		raw, _ := json.Marshal(map[string]string{"commit": p.Commit, "runtime": "pm2"})
		return Result{Status: "succeeded", Result: raw}
	}

	// docker: bring up the earlier image tag.
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
	if err != nil {
		// Surface the failure reason into the job log — otherwise a custom op that
		// returns fail(err) shows up in the panel with no explanation.
		log("stderr", "step "+stepName(step)+" failed: "+err.Error())
	}
	return err
}

// failStep logs the failure (step name + captured output + error) and returns a
// failed Result whose Error carries the step context, so the panel never shows a
// bare "exit status 1" with no logs.
func failStep(log Log, name, out string, err error) Result {
	if out != "" {
		log("stdout", out)
	}
	log("stderr", "step "+name+" failed: "+err.Error())
	return Result{Status: "failed", ExitCode: -1, Error: name + ": " + err.Error()}
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
