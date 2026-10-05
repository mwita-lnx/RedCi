package jobs

import (
	"encoding/base64"
	"path"
)

// gitTokenEnv builds the environment that feeds a short-lived clone token to
// git without it ever appearing on the command line (so it is not visible in
// `ps` or logs). Returns nil when no token is set.
func gitTokenEnv(token string) []string {
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

// InstallAppParams.Steps: install an existing app on a site, then ping.
func (p InstallAppParams) Steps(ctx PlanContext) ([]Step, error) {
	return []Step{
		{
			Name: "install app", Dir: p.BenchPath, Command: "bench",
			Args: []string{"--site", p.Site, "install-app", p.App},
		},
	}, nil
}

// GetFrappeAppParams.Steps: fetch a custom app onto a bench for the first time.
func (p GetFrappeAppParams) Steps(ctx PlanContext) ([]Step, error) {
	token := ctx.secret(p.CloneTok)
	return []Step{
		{
			Name: "get app", Dir: p.BenchPath, Command: "bench",
			Args: []string{"get-app", "--branch", p.Branch, "https://github.com/" + p.Repo},
			Env:  gitTokenEnv(token), Redact: []string{token},
		},
	}, nil
}

// DeployFrappeAppParams.Steps follows the spec's exact sequence. Maintenance
// mode (when requested) is turned on before migrate and ALWAYS turned off at
// the end as a deferred cleanup, even if an earlier step failed — the runner
// runs every step tagged AlwaysRun regardless of a prior failure.
func (p DeployFrappeAppParams) Steps(ctx PlanContext) ([]Step, error) {
	token := ctx.secret(p.CloneTok)
	appDir := path.Join(p.BenchPath, "apps", p.App)

	var steps []Step

	// 1. Backup each affected site.
	for _, site := range p.Sites {
		steps = append(steps, Step{
			Name: "backup " + site, Dir: p.BenchPath, Command: "bench",
			Args: []string{"--site", site, "backup"},
		})
	}
	// 2. Fetch and check out the exact commit.
	steps = append(steps,
		Step{
			Name: "fetch", Dir: appDir, Command: "git",
			Args: []string{"fetch", "origin", p.Branch},
			Env:  gitTokenEnv(token), Redact: []string{token},
		},
		Step{
			Name: "checkout", Dir: appDir, Command: "git",
			Args: []string{"checkout", "-B", p.Branch, p.Commit},
		},
		// 3. Python deps.
		Step{
			Name: "pip install", Dir: p.BenchPath, Command: "./env/bin/pip",
			Args: []string{"install", "--quiet", "-e", path.Join("apps", p.App)},
		},
	)
	// 4. Node deps only when the app has a package.json.
	if p.HasPackage {
		steps = append(steps, Step{
			Name: "yarn install", Dir: appDir, Command: "yarn",
			Args: []string{"install", "--frozen-lockfile"},
		})
	}
	// 5. Maintenance on (optional).
	if p.Maintenance {
		for _, site := range p.Sites {
			steps = append(steps, Step{
				Name: "maintenance on " + site, Dir: p.BenchPath, Command: "bench",
				Args: []string{"--site", site, "set-maintenance-mode", "on"},
			})
		}
	}
	// 6. Migrate each affected site.
	for _, site := range p.Sites {
		steps = append(steps, Step{
			Name: "migrate " + site, Dir: p.BenchPath, Command: "bench",
			Args: []string{"--site", site, "migrate"},
		})
	}
	// 7. Build, 8. Restart.
	steps = append(steps,
		Step{
			Name: "build", Dir: p.BenchPath, Command: "bench",
			Args: []string{"build", "--app", p.App},
		},
		Step{
			Name: "restart", Dir: p.BenchPath, Command: "bench",
			Args: []string{"restart"},
		},
	)
	// 9. Maintenance off — ALWAYS runs (deferred cleanup).
	if p.Maintenance {
		for _, site := range p.Sites {
			steps = append(steps, Step{
				Name: "maintenance off " + site, Dir: p.BenchPath, Command: "bench",
				Args:      []string{"--site", site, "set-maintenance-mode", "off"},
				AlwaysRun: true,
			})
		}
	}
	return steps, nil
}

// BackupSiteParams.Steps: a single bench backup, optionally with files.
func (p BackupSiteParams) Steps(ctx PlanContext) ([]Step, error) {
	args := []string{"--site", p.Site, "backup"}
	if p.WithFiles {
		args = append(args, "--with-files")
	}
	return []Step{
		{Name: "backup", Dir: p.BenchPath, Command: "bench", Args: args},
	}, nil
}
