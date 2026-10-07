package jobs

import (
	"fmt"
	"strings"
)

// SecretRef is a reference to a stored secret, e.g. "secret:db_root:bench:3".
// It never carries a plaintext value in params_json or the agent journal; the
// panel resolves it only in the claim response, over TLS.
type SecretRef string

// IsResolved reports whether this value has already been replaced with a real
// secret (the agent sees resolved values; the stored params never do).
func (s SecretRef) IsRef() bool { return strings.HasPrefix(string(s), "secret:") }

// ServerStatusParams has no inputs.
type ServerStatusParams struct{}

func (ServerStatusParams) Validate([]string) error { return nil }

// ListNginxConfigsParams has no inputs.
type ListNginxConfigsParams struct{}

func (ListNginxConfigsParams) Validate([]string) error { return nil }

// NewSiteParams drives `new_site`.
type NewSiteParams struct {
	BenchPath      string    `json:"bench_path"`
	Domain         string    `json:"domain"`
	Apps           []string  `json:"apps"`
	AdminPassword  SecretRef `json:"admin_password"`
	DBRootUser     string    `json:"db_root_user"`
	DBRootPassword SecretRef `json:"db_root_password"`
	WithSSL        bool      `json:"with_ssl"`
}

func (p NewSiteParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	if err := ValidateDomain(p.Domain); err != nil {
		return err
	}
	if len(p.Apps) == 0 {
		return fmt.Errorf("new_site: at least one app is required")
	}
	for _, a := range p.Apps {
		if err := ValidateAppName(a); err != nil {
			return err
		}
	}
	if p.AdminPassword == "" {
		return fmt.Errorf("new_site: admin_password missing")
	}
	if p.DBRootPassword == "" {
		return fmt.Errorf("new_site: db_root_password missing")
	}
	return nil
}

// DeleteSiteParams drives `delete_site`.
type DeleteSiteParams struct {
	BenchPath      string    `json:"bench_path"`
	Domain         string    `json:"domain"`
	DBRootUser     string    `json:"db_root_user"`
	DBRootPassword SecretRef `json:"db_root_password"`
}

func (p DeleteSiteParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	if p.DBRootPassword == "" {
		return fmt.Errorf("delete_site: db_root_password missing")
	}
	return ValidateDomain(p.Domain)
}

// SetupNginxParams drives `setup_nginx` — runs bench setup nginx + nginx -t + reload.
type SetupNginxParams struct {
	BenchPath string `json:"bench_path"`
	Domain    string `json:"domain"`
}

func (p SetupNginxParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	return ValidateDomain(p.Domain)
}

// InstallAppParams drives `install_app`.
type InstallAppParams struct {
	BenchPath string `json:"bench_path"`
	Site      string `json:"site"`
	App       string `json:"app"`
}

func (p InstallAppParams) Validate(roots []string) error {
	if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
		return err
	}
	if err := ValidateDomain(p.Site); err != nil {
		return err
	}
	return ValidateAppName(p.App)
}

// CertTarget is what a certificate is issued for.
type CertTarget string

const (
	CertTargetSite  CertTarget = "site"
	CertTargetRoute CertTarget = "route"
)

// IssueCertificateParams drives `issue_certificate`.
type IssueCertificateParams struct {
	Domain    string     `json:"domain"`
	Target    CertTarget `json:"target"`
	Email     string     `json:"email"`
	BenchPath string     `json:"bench_path,omitempty"` // required when Target == site
	TestCert  bool       `json:"test_cert"`            // --test-cert for dev/staging
}

func (p IssueCertificateParams) Validate(roots []string) error {
	if err := ValidateDomain(p.Domain); err != nil {
		return err
	}
	if err := ValidateEmail(p.Email); err != nil {
		return err
	}
	switch p.Target {
	case CertTargetSite:
		if err := ValidatePathUnder(p.BenchPath, roots); err != nil {
			return err
		}
	case CertTargetRoute:
		// no bench path
	default:
		return fmt.Errorf("issue_certificate: target must be site or route")
	}
	return nil
}
