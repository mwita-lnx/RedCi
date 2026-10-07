package jobs

// Steps for setup_nginx: bench setup nginx, nginx -t, nginx reload.
// Runs as a separate job after new_site so each can succeed/fail independently.
func (p SetupNginxParams) Steps(_ PlanContext) ([]Step, error) {
	return []Step{
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
