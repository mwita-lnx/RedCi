package jobs

// Steps for new_site, following the spec:
//  1. bench new-site <domain> with db-root + admin passwords
//  2. install each app (separate steps for clearer logs)
//  3. bench setup nginx --yes, nginx -t, reload
// The health check (ping) and SSL follow-up are handled by the runner/worker.
func (p NewSiteParams) Steps(ctx PlanContext) ([]Step, error) {
	admin := ctx.secret(p.AdminPassword)
	dbRoot := ctx.secret(p.DBRootPassword)
	dbUser := p.DBRootUser
	if dbUser == "" {
		dbUser = "root"
	}

	steps := []Step{
		{
			Name:    "create site",
			Dir:     p.BenchPath,
			Command: "bench",
			Args: []string{
				"new-site", p.Domain,
				"--db-root-username", dbUser,
				"--db-root-password", dbRoot,
				"--admin-password", admin,
				"--mariadb-user-host-login-scope=%",
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
	return steps, nil
}
