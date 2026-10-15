package jobs

// Steps for delete_site: drop the site then regenerate nginx config.
func (p DeleteSiteParams) Steps(ctx PlanContext) ([]Step, error) {
	dbUser := string(p.DBRootUser)
	if dbUser == "" {
		dbUser = "root"
	}
	steps := []Step{
		{
			Name:    "drop site",
			Dir:     p.BenchPath,
			Command: "bench",
			Args: []string{
				"drop-site", p.Domain,
				"--force", "--no-backup",
				"--db-root-username", dbUser,
				"--db-root-password", string(p.DBRootPassword),
			},
		},
		{
			Name:    "setup nginx",
			Dir:     p.BenchPath,
			Command: "bench",
			Args:    []string{"setup", "nginx", "--yes"},
		},
	}
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
