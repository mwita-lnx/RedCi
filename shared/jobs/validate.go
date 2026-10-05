// Package jobs defines every job type the panel can dispatch and the agent can
// run. It is imported by BOTH binaries so they agree on each job's shape and
// validation: the panel validates before inserting, the agent validates again
// before running. Nothing here executes commands.
package jobs

import (
	"fmt"
	"regexp"
	"strings"
)

// Validation patterns from the spec. They are deliberately strict: a value that
// does not match is rejected in the panel and again in the agent, which is the
// primary defence against command injection (there is no shell anywhere).
var (
	domainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	appRE    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	emailRE  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	// upstream: http://127.0.0.1:<port> by default (admin may override).
	upstreamRE = regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]{2,5}$`)
	nameRE     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`) // web app / server names
	// repo: owner/name, GitHub-style identifiers.
	repoRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?/[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)
	// branch: a conservative git ref (no spaces, no shell metacharacters, no ..).
	branchRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

// validateRepo checks an owner/name GitHub repo identifier.
func validateRepo(r string) error {
	if len(r) > 140 || !repoRE.MatchString(r) {
		return fmt.Errorf("repo %q: invalid (want owner/name)", r)
	}
	return nil
}

// validateBranch checks a git branch/ref name.
func validateBranch(b string) error {
	if b == "" {
		return fmt.Errorf("branch: empty")
	}
	if len(b) > 200 || strings.Contains(b, "..") || !branchRE.MatchString(b) {
		return fmt.Errorf("branch %q: invalid", b)
	}
	return nil
}

// ValidateDomain checks a DNS hostname.
func ValidateDomain(d string) error {
	if len(d) == 0 || len(d) > 253 {
		return fmt.Errorf("domain %q: length out of range", d)
	}
	if !domainRE.MatchString(d) {
		return fmt.Errorf("domain %q: invalid", d)
	}
	return nil
}

// ValidateAppName checks a Frappe app name.
func ValidateAppName(a string) error {
	if !appRE.MatchString(a) {
		return fmt.Errorf("app name %q: invalid (want ^[a-z][a-z0-9_]*$)", a)
	}
	return nil
}

// ValidateCommit checks a full 40-hex-char git commit.
func ValidateCommit(c string) error {
	if !commitRE.MatchString(c) {
		return fmt.Errorf("commit %q: must be 40 lowercase hex characters", c)
	}
	return nil
}

// ValidateEmail checks a Let's Encrypt contact email.
func ValidateEmail(e string) error {
	if len(e) > 254 || !emailRE.MatchString(e) {
		return fmt.Errorf("email %q: invalid", e)
	}
	return nil
}

// ValidateName checks a web-app or server name (also a compose project name).
func ValidateName(n string) error {
	if len(n) == 0 || len(n) > 63 || !nameRE.MatchString(n) {
		return fmt.Errorf("name %q: invalid (want ^[a-z][a-z0-9-]*$)", n)
	}
	return nil
}

// ValidatePathUnder ensures p is clean, absolute, and sits under one of the
// allowlisted roots. It rejects traversal (..) and relative paths.
func ValidatePathUnder(p string, roots []string) error {
	if p == "" || !strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q: must be absolute", p)
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("path %q: must not contain ..", p)
	}
	// Normalise trailing slash for the prefix check.
	clean := strings.TrimRight(p, "/")
	for _, root := range roots {
		root = strings.TrimRight(root, "/")
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return nil
		}
	}
	return fmt.Errorf("path %q: not under an allowlisted root", p)
}

// ValidateUpstream checks a proxy upstream. adminOverride allows any value an
// admin explicitly entered (still guarded by nginx -t downstream).
func ValidateUpstream(u string, adminOverride bool) error {
	if adminOverride {
		if u == "" {
			return fmt.Errorf("upstream: empty")
		}
		return nil
	}
	if !upstreamRE.MatchString(u) {
		return fmt.Errorf("upstream %q: must be http://127.0.0.1:<port>", u)
	}
	return nil
}

// ValidatePort checks a TCP port in the usable range.
func ValidatePort(p int) error {
	if p < 1 || p > 65535 {
		return fmt.Errorf("port %d: out of range", p)
	}
	return nil
}
