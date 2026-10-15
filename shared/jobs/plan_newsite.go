package jobs

import "strings"

// Steps for new_site. Modeled on Frappe Press's `new_site` job
// (frappe/agent bench.py), which runs the whole site lifecycle as one unit:
//
//	bench_new_site -> install_apps -> update_config -> enable_scheduler -> setup_nginx -> reload_nginx
//
// We follow the same order and fold nginx back in (it used to be a separate
// setup_nginx job) so one deploy == one job == one log stream, the way Press
// does it. The health check (ping) and SSL follow-up are handled downstream.
func (p NewSiteParams) Steps(ctx PlanContext) ([]Step, error) {
	admin := ctx.secret(p.AdminPassword)
	dbRoot := ctx.secret(p.DBRootPassword)
	dbUser := p.DBRootUser
	if dbUser == "" {
		dbUser = "root"
	}

	steps := []Step{
		// Press runs `bench new-site --no-mariadb-socket ... --db-name <db>`.
		// --no-mariadb-socket forces TCP auth (works when the agent is not the
		// mysql unix-socket owner); --db-name pins the database name to the site.
		{
			Name:    "create site",
			Dir:     p.BenchPath,
			Command: "bench",
			Args: []string{
				"new-site", p.Domain,
				"--no-mariadb-socket",
				"--db-name", siteDBName(p.Domain),
				"--db-root-username", dbUser,
				"--db-root-password", dbRoot,
				"--admin-password", admin,
				"--mariadb-user-host-login-scope=%",
			},
			Redact: []string{dbRoot, admin},
		},
	}

	// install_apps: one step per app for clear logs. Press uses
	// `install-app <app> --force` and falls back to the un-forced form on
	// benches older than v14 (where --force doesn't exist). We mirror that
	// exactly via the runner's OnErrorContains/Fallback mechanism.
	for _, app := range p.Apps {
		if app == "frappe" {
			continue // Press skips frappe; it is installed by new-site itself.
		}
		steps = append(steps, installAppStep(p.BenchPath, p.Domain, app))
	}

	// update_config: apply any extra site_config keys (Press's site.update_config).
	for _, k := range sortedKeys(p.Config) {
		steps = append(steps, Step{
			Name:    "set config " + k,
			Dir:     p.BenchPath,
			Command: "bench",
			Args:    []string{"--site", p.Domain, "set-config", k, p.Config[k]},
		})
	}

	// enable_scheduler: Press enables the scheduler so background jobs run.
	steps = append(steps, Step{
		Name:    "enable scheduler",
		Dir:     p.BenchPath,
		Command: "bench",
		Args:    []string{"--site", p.Domain, "enable-scheduler"},
	})

	// setup_nginx + reload: folded in from the old separate setup_nginx job so
	// the whole create is one job, matching Press's new_site.
	steps = append(steps, Step{
		Name:    "setup nginx",
		Dir:     p.BenchPath,
		Command: "bench",
		Args:    []string{"setup", "nginx", "--yes"},
	})
	if ctx.NginxVersion != "" {
		steps = append(steps,
			Step{
				Name:    "nginx test",
				Command: "sudo",
				Args:    []string{"/usr/sbin/nginx", "-t"},
			},
			Step{
				Name:    "nginx reload",
				Command: "sudo",
				Args:    []string{"/usr/bin/systemctl", "reload", "nginx"},
			},
		)
	}
	return steps, nil
}

// installAppStep builds a `bench --site <domain> install-app <app>` step that
// mirrors Press: try `--force`, and if the bench is too old to know that flag
// ("no such option: --force"), fall back to the plain form.
func installAppStep(benchPath, domain, app string) Step {
	plain := func() *Step {
		return &Step{
			Name:    "install app " + app + " (no --force)",
			Dir:     benchPath,
			Command: "bench",
			Args:    []string{"--site", domain, "install-app", app},
		}
	}
	return Step{
		Name:    "install app " + app,
		Dir:     benchPath,
		Command: "bench",
		Args:    []string{"--site", domain, "install-app", app, "--force"},
		// Press catches: "Error: no such option: --force".
		OnErrorContains: "no such option: --force",
		Fallback:        plain(),
	}
}

// siteDBName derives a deterministic database name from the domain, the way a
// db-name is pinned per site. Frappe truncates to 16 chars and disallows dots.
func siteDBName(domain string) string {
	name := strings.NewReplacer(".", "_", "-", "_").Replace(domain)
	if len(name) > 16 {
		name = name[:16]
	}
	return name
}

// sortedKeys returns the map keys in deterministic order so the step plan (and
// its golden tests) are stable.
func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort — tiny maps, avoids importing sort for one call site
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
