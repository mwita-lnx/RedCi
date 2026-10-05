package jobs

// Steps for issue_certificate. All certbot calls go through the root-owned
// ops-certbot wrapper (never certbot directly), which runs certonly with a
// fixed deploy hook. For a site, bench is then pointed at the new cert; for a
// route, the runner re-renders the route with SSL after this job.
func (p IssueCertificateParams) Steps(ctx PlanContext) ([]Step, error) {
	certArgs := []string{"/usr/local/sbin/ops-certbot", "issue", p.Domain, p.Email}
	if p.TestCert {
		certArgs = append(certArgs, "--test-cert")
	}
	steps := []Step{
		{
			Name:    "issue certificate",
			Command: "sudo",
			Args:    certArgs,
		},
	}
	if p.Target == CertTargetSite {
		live := "/etc/letsencrypt/live/" + p.Domain
		steps = append(steps,
			Step{
				Name:    "set ssl certificate",
				Dir:     p.BenchPath,
				Command: "bench",
				Args:    []string{"set-ssl-certificate", p.Domain, live + "/fullchain.pem"},
			},
			Step{
				Name:    "set ssl key",
				Dir:     p.BenchPath,
				Command: "bench",
				Args:    []string{"set-ssl-key", p.Domain, live + "/privkey.pem"},
			},
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
	}
	return steps, nil
}
