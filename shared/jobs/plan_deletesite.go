package jobs

// Steps for delete_site: drop the site then regenerate nginx config.
func (p DeleteSiteParams) Steps(_ PlanContext) ([]Step, error) {
	dbUser := string(p.DBRootUser)
	if dbUser == "" {
		dbUser = "root"
	}
	return []Step{
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
		{
			Name:    "nginx test",
			Command: "sudo",
			Args:    []string{"/usr/sbin/nginx", "-t"},
		},
		{
			Name:    "nginx reload",
			Command: "sudo",
			Args:    []string{"/usr/bin/systemctl", "reload", "nginx"},
		},
	}, nil
}
