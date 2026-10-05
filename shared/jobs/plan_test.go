package jobs

import (
	"strings"
	"testing"
)

func TestNewSiteSteps(t *testing.T) {
	p := NewSiteParams{
		BenchPath: "/home/frappe/frappe-bench", Domain: "client1.example.com",
		Apps: []string{"erpnext", "hrms"}, AdminPassword: "secret:admin:1", DBRootPassword: "secret:db_root:1",
	}
	ctx := PlanContext{Secrets: map[SecretRef]string{
		"secret:admin:1":   "ADMINPW",
		"secret:db_root:1": "DBPW",
	}}
	steps, err := p.Steps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Expect: create + 2 installs + setup nginx + test + reload = 6 steps.
	if len(steps) != 6 {
		t.Fatalf("got %d steps, want 6", len(steps))
	}
	if steps[0].Command != "bench" || steps[0].Args[0] != "new-site" || steps[0].Args[1] != "client1.example.com" {
		t.Fatalf("step 0 argv wrong: %v", steps[0].Args)
	}
	// Secret must be resolved into argv and listed for redaction.
	joined := strings.Join(steps[0].Args, " ")
	if !strings.Contains(joined, "DBPW") || !strings.Contains(joined, "ADMINPW") {
		t.Fatalf("secrets not resolved into argv: %v", steps[0].Args)
	}
	if len(steps[0].Redact) != 2 {
		t.Fatalf("expected 2 redaction values, got %v", steps[0].Redact)
	}
	if steps[1].Args[2] != "install-app" || steps[1].Args[3] != "erpnext" {
		t.Fatalf("install step wrong: %v", steps[1].Args)
	}
	// No shell anywhere.
	for _, s := range steps {
		if s.Command == "sh" || s.Command == "bash" {
			t.Fatalf("step %q uses a shell", s.Name)
		}
	}
}

func TestIssueCertSiteSteps(t *testing.T) {
	p := IssueCertificateParams{
		Domain: "client1.example.com", Target: CertTargetSite,
		Email: "ops@example.com", BenchPath: "/home/frappe/frappe-bench", TestCert: true,
	}
	steps, err := p.Steps(PlanContext{})
	if err != nil {
		t.Fatal(err)
	}
	// issue + set cert + set key + setup nginx + test + reload = 6.
	if len(steps) != 6 {
		t.Fatalf("got %d steps, want 6", len(steps))
	}
	if steps[0].Args[0] != "/usr/local/sbin/ops-certbot" || steps[0].Args[1] != "issue" {
		t.Fatalf("cert step argv wrong: %v", steps[0].Args)
	}
	last := strings.Join(steps[0].Args, " ")
	if !strings.Contains(last, "--test-cert") {
		t.Fatalf("test-cert flag missing: %v", steps[0].Args)
	}
}
