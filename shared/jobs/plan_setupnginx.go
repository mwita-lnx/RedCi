package jobs

// Steps for setup_nginx: bench setup nginx, nginx -t, nginx reload.
// Runs as a separate job after new_site so each can succeed/fail independently.
// When nginx is not detected on the host (NginxVersion == "") the test and
// reload steps are skipped — this keeps dev-Mac setups working where bench
// manages its own server and system nginx is absent.
func (p SetupNginxParams) Steps(ctx PlanContext) ([]Step, error) {
	steps := []Step{
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
