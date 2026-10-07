package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// backupPathRe matches the "Database: <path>" line bench prints at the end of a
// successful `bench backup`, e.g.
//   Database  : /home/f/bench/sites/site/private/backups/20260101_000000-site-database.sql.gz
var backupPathRe = regexp.MustCompile(`(?mi)^\s*(?:config|database|public files|private files)\s*:?\s+(\S+\.(?:sql\.gz|sql))\s*$`)

// DeployFrappeResult is the structured result the panel stores so a later
// rollback knows the commit to return to and the pre-deploy backups to restore.
type DeployFrappeResult struct {
	App          string            `json:"app"`
	Commit       string            `json:"commit"`        // the commit we deployed to
	PrevCommit   string            `json:"prev_commit"`   // commit before this deploy
	Backups      map[string]string `json:"backups"`       // site -> db backup path (pre-deploy)
}

// runDeployFrappeApp runs the deploy sequence as a custom op so it can capture
// the previous commit and the pre-deploy database backups into result_json for
// rollback. The step sequence mirrors the spec: backup -> fetch -> checkout ->
// deps -> (maintenance) -> migrate -> build -> restart -> (maintenance off).
func runDeployFrappeApp(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.DeployFrappeAppParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	token := frappeCloneToken(job.Params)
	appDir := path.Join(p.BenchPath, "apps", p.App)

	// 0. Record the commit we are moving away from (rollback target).
	prevCommit := gitHead(ctx, env, appDir, log)

	// 1. Backup each affected site BEFORE touching code; capture the db path.
	backups := map[string]string{}
	for _, site := range p.Sites {
		out, _, err := env.Runner.Exec(ctx, jobs.Step{
			Name: "backup " + site, Dir: p.BenchPath, Command: "bench",
			Args: []string{"--site", site, "backup"},
		}, 30*time.Minute)
		log("stdout", out)
		if err != nil {
			return fail(err)
		}
		if dbPath := parseBackupDBPath(out); dbPath != "" {
			backups[site] = dbPath
			log("system", "captured pre-deploy backup for "+site+": "+dbPath)
		} else {
			log("stderr", "could not parse backup path for "+site+"; rollback DB restore may be unavailable")
		}
	}

	// 2. Fetch + checkout the exact commit.
	fetch := jobs.Step{
		Name: "fetch", Dir: appDir, Command: "git",
		Args: []string{"fetch", "origin", p.Branch},
		Env:  frappeGitEnv(token), Redact: []string{token},
	}
	if err := runStepLogged(ctx, env, fetch, log, 10*time.Minute); err != nil {
		return fail(err)
	}
	checkout := jobs.Step{
		Name: "checkout", Dir: appDir, Command: "git",
		Args: []string{"checkout", "-B", p.Branch, p.Commit},
	}
	if err := runStepLogged(ctx, env, checkout, log, 5*time.Minute); err != nil {
		return fail(err)
	}

	// 3-9. Deps, migrate, build, restart (maintenance optional, off deferred).
	if res := frappeApplyAndMigrate(ctx, env, applyParams{
		BenchPath:   p.BenchPath,
		App:         p.App,
		AppDir:      appDir,
		Sites:       p.Sites,
		HasPackage:  p.HasPackage,
		Maintenance: p.Maintenance,
	}, log); res.Status != "succeeded" {
		return res
	}

	result := DeployFrappeResult{
		App: p.App, Commit: p.Commit, PrevCommit: prevCommit, Backups: backups,
	}
	raw, _ := json.Marshal(result)
	log("system", "deploy complete; previous commit recorded as "+shortCommit(prevCommit))
	return Result{Status: "succeeded", Result: raw}
}

// runRollbackFrappeApp checks out the previous commit, reinstalls deps, restores
// each site's pre-deploy database backup, then migrates/builds/restarts.
func runRollbackFrappeApp(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	var p jobs.RollbackFrappeAppParams
	if err := json.Unmarshal(job.Params, &p); err != nil {
		return fail(err)
	}
	appDir := path.Join(p.BenchPath, "apps", p.App)

	// 1. Check out the previous commit (local; no fetch needed).
	checkout := jobs.Step{
		Name: "checkout previous", Dir: appDir, Command: "git",
		Args: []string{"checkout", "-B", p.Branch, p.Commit},
	}
	if err := runStepLogged(ctx, env, checkout, log, 5*time.Minute); err != nil {
		return fail(err)
	}

	// 2. Restore each site's pre-deploy database backup (code+DB rollback).
	//    Always on, so the site returns to the exact pre-deploy state.
	for _, site := range p.Sites {
		dbPath := p.Backups[site]
		if dbPath == "" {
			log("stderr", "no backup recorded for "+site+"; skipping DB restore (code-only for this site)")
			continue
		}
		restore := jobs.Step{
			Name: "restore " + site, Dir: p.BenchPath, Command: "bench",
			Args: []string{"--site", site, "--force", "restore", dbPath},
		}
		if err := runStepLogged(ctx, env, restore, log, 30*time.Minute); err != nil {
			return fail(err)
		}
	}

	// 3. Reinstall deps + migrate + build + restart on the restored code.
	if res := frappeApplyAndMigrate(ctx, env, applyParams{
		BenchPath:   p.BenchPath,
		App:         p.App,
		AppDir:      appDir,
		Sites:       p.Sites,
		HasPackage:  p.HasPackage,
		Maintenance: true, // rollback always uses maintenance mode
	}, log); res.Status != "succeeded" {
		return res
	}

	raw, _ := json.Marshal(map[string]string{"app": p.App, "commit": p.Commit})
	log("system", "rollback complete to "+shortCommit(p.Commit))
	return Result{Status: "succeeded", Result: raw}
}

// applyParams is the shared tail of deploy and rollback: deps + migrate + build.
type applyParams struct {
	BenchPath   string
	App         string
	AppDir      string
	Sites       []string
	HasPackage  bool
	Maintenance bool
}

// frappeApplyAndMigrate runs: pip install, (yarn), maintenance on, migrate,
// build, restart, maintenance off. Maintenance-off always runs on exit.
func frappeApplyAndMigrate(ctx context.Context, env Env, p applyParams, log Log) Result {
	// Maintenance-off deferred so a mid-migrate failure still lifts the lock.
	maintenanceOff := func() {
		if !p.Maintenance {
			return
		}
		for _, site := range p.Sites {
			step := jobs.Step{
				Name: "maintenance off " + site, Dir: p.BenchPath, Command: "bench",
				Args: []string{"--site", site, "set-maintenance-mode", "off"},
			}
			_ = runStepLogged(ctx, env, step, log, 2*time.Minute)
		}
	}

	// pip install the app (editable).
	pip := jobs.Step{
		Name: "pip install", Dir: p.BenchPath, Command: "./env/bin/pip",
		Args: []string{"install", "--quiet", "-e", path.Join("apps", p.App)},
	}
	if err := runStepLogged(ctx, env, pip, log, 15*time.Minute); err != nil {
		return fail(err)
	}
	if p.HasPackage {
		yarn := jobs.Step{
			Name: "yarn install", Dir: p.AppDir, Command: "yarn",
			Args: []string{"install", "--frozen-lockfile"},
		}
		if err := runStepLogged(ctx, env, yarn, log, 15*time.Minute); err != nil {
			return fail(err)
		}
	}

	if p.Maintenance {
		for _, site := range p.Sites {
			step := jobs.Step{
				Name: "maintenance on " + site, Dir: p.BenchPath, Command: "bench",
				Args: []string{"--site", site, "set-maintenance-mode", "on"},
			}
			if err := runStepLogged(ctx, env, step, log, 2*time.Minute); err != nil {
				maintenanceOff()
				return fail(err)
			}
		}
	}

	for _, site := range p.Sites {
		mig := jobs.Step{
			Name: "migrate " + site, Dir: p.BenchPath, Command: "bench",
			Args: []string{"--site", site, "migrate"},
		}
		if err := runStepLogged(ctx, env, mig, log, 30*time.Minute); err != nil {
			maintenanceOff()
			return fail(err)
		}
	}

	build := jobs.Step{
		Name: "build", Dir: p.BenchPath, Command: "bench",
		Args: []string{"build", "--app", p.App},
	}
	if err := runStepLogged(ctx, env, build, log, 15*time.Minute); err != nil {
		maintenanceOff()
		return fail(err)
	}
	restart := jobs.Step{
		Name: "restart", Dir: p.BenchPath, Command: "bench", Args: []string{"restart"},
	}
	if err := runStepLogged(ctx, env, restart, log, 5*time.Minute); err != nil {
		maintenanceOff()
		return fail(err)
	}
	maintenanceOff()
	return Result{Status: "succeeded"}
}

// gitHead returns the current HEAD commit of appDir, or "" if it cannot be read.
func gitHead(ctx context.Context, env Env, appDir string, log Log) string {
	out, _, err := env.Runner.Exec(ctx, jobs.Step{
		Name: "record current commit", Dir: appDir, Command: "git",
		Args: []string{"rev-parse", "HEAD"},
	}, 1*time.Minute)
	if err != nil {
		log("stderr", "could not read current commit; rollback will be unavailable")
		return ""
	}
	return strings.TrimSpace(out)
}

// parseBackupDBPath extracts the database backup file path from `bench backup`
// output. bench prints a summary with a "Database: <path>" line.
func parseBackupDBPath(out string) string {
	for _, m := range backupPathRe.FindAllStringSubmatch(out, -1) {
		p := strings.TrimSpace(m[1])
		if strings.HasSuffix(p, "-database.sql.gz") || strings.HasSuffix(p, "-database.sql") ||
			strings.Contains(p, "database") {
			return p
		}
	}
	return ""
}

func shortCommit(c string) string {
	if len(c) >= 8 {
		return c[:8]
	}
	if c == "" {
		return "(unknown)"
	}
	return c
}

// frappeGitEnv builds the git clone-token environment (mirrors plan_frappe.go's
// gitTokenEnv; kept local to the custom op so the plan file can be removed).
func frappeGitEnv(token string) []string {
	if token == "" {
		return nil
	}
	auth := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=" + auth,
	}
}

// frappeCloneToken pulls the resolved clone_token out of the (already
// secret-resolved) params JSON without re-declaring the whole struct.
func frappeCloneToken(raw json.RawMessage) string {
	var v struct {
		CloneTok string `json:"clone_token"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.CloneTok
}
