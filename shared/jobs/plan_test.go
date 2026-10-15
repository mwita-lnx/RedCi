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
	// Press-style single job (nginx folded in). With NginxVersion unset, the
	// nginx test+reload steps are skipped. Expect:
	//   create + install erpnext + install hrms + enable scheduler + setup nginx
	//   = 5 steps.
	if len(steps) != 5 {
		t.Fatalf("got %d steps, want 5: %v", len(steps), stepNames(steps))
	}
	if steps[0].Command != "bench" || steps[0].Args[0] != "new-site" || steps[0].Args[1] != "client1.example.com" {
		t.Fatalf("step 0 argv wrong: %v", steps[0].Args)
	}
	// Press's new-site flags: --no-mariadb-socket and an explicit --db-name.
	joined := strings.Join(steps[0].Args, " ")
	if !strings.Contains(joined, "--no-mariadb-socket") {
		t.Fatalf("missing --no-mariadb-socket: %v", steps[0].Args)
	}
	if !strings.Contains(joined, "--db-name") {
		t.Fatalf("missing --db-name: %v", steps[0].Args)
	}
	// Secret must be resolved into argv and listed for redaction.
	if !strings.Contains(joined, "DBPW") || !strings.Contains(joined, "ADMINPW") {
		t.Fatalf("secrets not resolved into argv: %v", steps[0].Args)
	}
	if len(steps[0].Redact) != 2 {
		t.Fatalf("expected 2 redaction values, got %v", steps[0].Redact)
	}
	// install-app uses --force with a v14 fallback, mirroring Press.
	if steps[1].Args[2] != "install-app" || steps[1].Args[3] != "erpnext" || steps[1].Args[4] != "--force" {
		t.Fatalf("install step wrong: %v", steps[1].Args)
	}
	if steps[1].OnErrorContains != "no such option: --force" || steps[1].Fallback == nil {
		t.Fatalf("install step missing --force fallback: %+v", steps[1])
	}
	if got := steps[1].Fallback.Args; got[len(got)-1] == "--force" {
		t.Fatalf("fallback should not carry --force: %v", got)
	}
	// Scheduler + nginx are folded into the one job.
	if steps[3].Args[2] != "enable-scheduler" {
		t.Fatalf("expected enable-scheduler step, got: %v", steps[3].Args)
	}
	if steps[4].Name != "setup nginx" {
		t.Fatalf("expected setup nginx step, got: %q", steps[4].Name)
	}
	// No shell anywhere.
	for _, s := range steps {
		if s.Command == "sh" || s.Command == "bash" {
			t.Fatalf("step %q uses a shell", s.Name)
		}
	}
}

func stepNames(steps []Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Name
	}
	return out
}

func TestIssueCertSiteSteps(t *testing.T) {
	p := IssueCertificateParams{
		Domain: "client1.example.com", Target: CertTargetSite,
		Email: "ops@example.com", BenchPath: "/home/frappe/frappe-bench", TestCert: true,
	}
	// nginx test+reload are gated on a detected nginx version, so provide one.
	steps, err := p.Steps(PlanContext{NginxVersion: "1.24.0"})
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
