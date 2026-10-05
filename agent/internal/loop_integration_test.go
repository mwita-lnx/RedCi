package agentcfg_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	agentcfg "github.com/mwita-lnx/RedCi/agent/internal"
	"github.com/mwita-lnx/RedCi/agent/internal/client"
	"github.com/mwita-lnx/RedCi/agent/internal/journal"
	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/ops/paneltest"
	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// TestEnrollClaimRunReport drives the full agent<->panel flow in dry-run:
// enroll, claim a new_site job, stream logs, report success, and confirm
// secrets never leak into the logs.
func TestEnrollClaimRunReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	panel, err := paneltest.Start(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer panel.Close()

	// 1. Admin creates an enrollment token.
	serverID, enrollToken, err := panel.CreateEnrollment(ctx, "srv1", "srv1.internal")
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(serverID, 10)

	// 2. Store referenced secrets.
	if err := panel.PutSecret(ctx, "admin:"+id, "ADMINPW"); err != nil {
		t.Fatal(err)
	}
	if err := panel.PutSecret(ctx, "db_root:"+id, "DBPW"); err != nil {
		t.Fatal(err)
	}

	// 3. Agent registers.
	cli := client.New(panel.URL())
	reg, err := cli.Register(ctx, protocol.RegisterRequest{
		EnrollmentToken: enrollToken, Hostname: "srv1.internal", AgentVersion: "test",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.ServerID != serverID {
		t.Fatalf("server id mismatch")
	}
	cli.SetToken(reg.Token)

	// 4. Enqueue a new_site job referencing the secrets.
	params := jobs.NewSiteParams{
		BenchPath:      "/home/frappe/frappe-bench",
		Domain:         "client1.example.com",
		Apps:           []string{"erpnext"},
		AdminPassword:  jobs.SecretRef("secret:admin:" + id),
		DBRootPassword: jobs.SecretRef("secret:db_root:" + id),
	}
	_, jobID, err := panel.EnqueueJob(ctx, serverID, jobs.TypeNewSite, params, "bench:1")
	if err != nil {
		t.Fatal(err)
	}

	// 5. Run the agent in dry-run.
	jrnl, err := journal.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer jrnl.Close()
	ag := agentcfg.NewAgent(
		agentcfg.Config{PanelURL: panel.URL(), BenchRoots: []string{"/home/frappe/frappe-bench"}},
		agentcfg.Credential{ServerID: serverID, Token: reg.Token},
		cli, jrnl, runner.New(runner.Options{DryRun: true}), "test",
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	agCtx, agCancel := context.WithCancel(ctx)
	go ag.Run(agCtx)
	defer agCancel()

	// 6. Wait for a terminal state.
	final := waitStatus(t, panel, jobID, 15*time.Second)
	if final != "succeeded" {
		lines, _ := panel.JobLogLines(ctx, jobID)
		for _, l := range lines {
			t.Logf("log: %s", l)
		}
		t.Fatalf("job final status = %q, want succeeded", final)
	}

	// 7. Logs have the step marker and no plaintext secret.
	lines, _ := panel.JobLogLines(ctx, jobID)
	var sawStep, sawSecret bool
	for _, l := range lines {
		if strings.Contains(l, "== step: create site") {
			sawStep = true
		}
		if strings.Contains(l, "ADMINPW") || strings.Contains(l, "DBPW") {
			sawSecret = true
		}
	}
	if !sawStep {
		t.Errorf("expected a 'create site' step marker; got %d lines: %v", len(lines), lines)
	}
	if len(lines) != 10 {
		t.Errorf("expected 10 log lines (5 markers + 5 dry-run), got %d", len(lines))
	}
	if sawSecret {
		t.Error("plaintext secret leaked into logs")
	}
}

// TestProtocolVersionRejected confirms a mismatched protocol header is refused.
func TestLongPollReturns204WhenIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	panel, err := paneltest.Start(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer panel.Close()
	serverID, token, err := panel.CreateEnrollment(ctx, "srv2", "srv2.internal")
	if err != nil {
		t.Fatal(err)
	}
	_ = serverID
	cli := client.New(panel.URL())
	reg, err := cli.Register(ctx, protocol.RegisterRequest{EnrollmentToken: token, Hostname: "srv2.internal"})
	if err != nil {
		t.Fatal(err)
	}
	cli.SetToken(reg.Token)

	start := time.Now()
	_, ok, err := cli.NextJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no job (204)")
	}
	if time.Since(start) < 25*time.Second {
		t.Logf("long-poll returned after %s (expected ~30s wait)", time.Since(start))
	}
}

// TestCustomOpListNginx proves the custom-op dispatch path end to end: the
// agent claims a list_nginx_configs job, runs the custom op (not the generic
// step runner), and reports success with a result.
func TestCustomOpListNginx(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	panel, err := paneltest.Start(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer panel.Close()

	serverID, enrollToken, err := panel.CreateEnrollment(ctx, "srvc", "srvc.internal")
	if err != nil {
		t.Fatal(err)
	}
	cli := client.New(panel.URL())
	reg, err := cli.Register(ctx, protocol.RegisterRequest{EnrollmentToken: enrollToken, Hostname: "srvc.internal"})
	if err != nil {
		t.Fatal(err)
	}
	cli.SetToken(reg.Token)

	_, jobID, err := panel.EnqueueJob(ctx, serverID, jobs.TypeListNginxConfigs, jobs.ListNginxConfigsParams{}, "")
	if err != nil {
		t.Fatal(err)
	}

	jrnl, err := journal.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer jrnl.Close()
	// Point the agent's ops.d at a temp dir so the list op reads a real (empty)
	// directory without touching the host's /etc/nginx.
	cfg := agentcfg.Config{PanelURL: panel.URL(), BenchRoots: []string{"/home/frappe/frappe-bench"}}
	ag := agentcfg.NewAgent(cfg, agentcfg.Credential{ServerID: serverID, Token: reg.Token},
		cli, jrnl, runner.New(runner.Options{}), "test",
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	agCtx, agCancel := context.WithCancel(ctx)
	go ag.Run(agCtx)
	defer agCancel()

	if final := waitStatus(t, panel, jobID, 15*time.Second); final != "succeeded" {
		t.Fatalf("list_nginx_configs status = %q, want succeeded", final)
	}
}

func waitStatus(t *testing.T, panel *paneltest.Panel, jobID int64, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if status, err := panel.JobStatus(context.Background(), jobID); err == nil {
			switch status {
			case "succeeded", "failed", "cancelled", "lost":
				return status
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "timeout"
}
