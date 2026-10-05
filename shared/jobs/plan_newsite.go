package jobs

// Steps for new_site, following the spec:
//  1. bench new-site <domain> with db-root + admin passwords
//  2. install each app (separate steps for clearer logs)
//  3. bench setup nginx --yes, nginx -t, reload
// The health check (ping) and SSL follow-up are handled by the runner/worker.
func (p NewSiteParams) Steps(ctx PlanContext) ([]Step, error) {
	admin := ctx.secret(p.AdminPassword)
	dbRoot := ctx.secret(p.DBRootPassword)

	steps := []Step{
		{
			Name:    "create site",
			Dir:     p.BenchPath,
			Command: "bench",
			Args: []string{
				"new-site", p.Domain,
				"--db-root-username", "ops_admin",
				"--db-root-password", dbRoot,
				"--admin-password", admin,
			},
			Redact: []string{dbRoot, admin},
		},
	}
	for _, app := range p.Apps {
		steps = append(steps, Step{
			Name:    "install app " + app,
			Dir:     p.BenchPath,
			Command: "bench",
			Args:    []string{"--site", p.Domain, "install-app", app},
		})
	}
	steps = append(steps,
		Step{
			Name:    "setup nginx",
			Dir:     p.BenchPath,
			Command: "bench",
			Args:    []string{"setup", "nginx", "--yes"},
		},
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
	return steps, nil
}
