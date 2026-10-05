// Command ops-certbot is a root-owned wrapper the agent may call through sudo.
// It accepts ONLY `issue <domain> <email> [--test-cert]` or `list`, validates
// its arguments with the shared validators, and runs a FIXED certbot command
// with a fixed --deploy-hook. This prevents the agent (running as frappe) from
// using certbot's hooks to gain root.
//
// Install as /usr/local/sbin/ops-certbot (root:root, 0755) and allow it in
// /etc/sudoers.d/ops-agent. See deploy/sudoers.ops-agent.
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mwita-lnx/RedCi/shared/jobs"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ops-certbot:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ops-certbot issue <domain> <email> [--test-cert] | list")
	}
	switch args[0] {
	case "list":
		return exec.Command("certbot", "certificates").Run()
	case "issue":
		return issue(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func issue(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("issue requires <domain> <email>")
	}
	domain, email := args[0], args[1]
	testCert := false
	for _, a := range args[2:] {
		switch a {
		case "--test-cert":
			testCert = true
		default:
			return fmt.Errorf("unexpected argument %q", a)
		}
	}
	// Validate with the SHARED validators, so the agent and this wrapper agree
	// and a hostile domain/email can never reach certbot.
	if err := jobs.ValidateDomain(domain); err != nil {
		return err
	}
	if err := jobs.ValidateEmail(email); err != nil {
		return err
	}

	certbotArgs := []string{
		"certonly", "--nginx",
		"--non-interactive", "--agree-tos",
		"-m", email,
		"-d", domain,
		"--deploy-hook", "systemctl reload nginx",
	}
	if testCert {
		certbotArgs = append(certbotArgs, "--test-cert")
	}
	cmd := exec.Command("certbot", certbotArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
