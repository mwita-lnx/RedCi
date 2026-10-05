// Package paneltest provides a fully wired, in-memory panel for integration
// tests. It lives outside internal/ so tests in the agent tree can start a real
// panel and exercise the agent<->panel protocol end to end.
package paneltest

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"

	ops "github.com/mwita-lnx/RedCi/ops/internal"
	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/secrets"
	"github.com/mwita-lnx/RedCi/ops/internal/server"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/internal/worker"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// Panel is a running panel for tests: HTTP server, worker and store.
type Panel struct {
	DB     *store.DB
	Server *server.Server
	Worker *worker.Worker
	HTTP   *httptest.Server
}

// Start builds and starts a panel backed by a temp SQLite file in dir. The
// worker is started on ctx; call Close when done.
func Start(ctx context.Context, dir string) (*Panel, error) {
	db, err := store.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	sealer, err := secrets.NewSealer(make([]byte, 32))
	if err != nil {
		db.Close()
		return nil, err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus := events.New()
	srv := server.New(ops.Config{Dev: true, BaseURL: "http://127.0.0.1"}, db, auth.NewService(db, sealer), sealer, bus, log)
	wrk := worker.New(db, bus, srv, log)
	ts := httptest.NewServer(srv.Handler())
	go wrk.Run(ctx)
	return &Panel{DB: db, Server: srv, Worker: wrk, HTTP: ts}, nil
}

// Close shuts the panel down.
func (p *Panel) Close() {
	p.HTTP.Close()
	p.DB.Close()
}

// URL is the base URL agents connect to.
func (p *Panel) URL() string { return p.HTTP.URL }

// CreateEnrollment proxies the server helper.
func (p *Panel) CreateEnrollment(ctx context.Context, name, hostname string) (int64, string, error) {
	return p.Server.CreateEnrollment(ctx, name, hostname)
}

// PutSecret proxies the server helper.
func (p *Panel) PutSecret(ctx context.Context, name, value string) error {
	return p.Server.PutSecretForTest(ctx, name, value)
}

// EnqueueJob proxies the server helper, nudging the worker.
func (p *Panel) EnqueueJob(ctx context.Context, serverID int64, t jobs.Type, params any, lockKey string) (int64, int64, error) {
	return p.Server.EnqueueJob(ctx, serverID, t, params, lockKey, p.Worker)
}

// JobStatus returns a job's current status, so a test can poll for completion
// without importing the internal store.
func (p *Panel) JobStatus(ctx context.Context, jobID int64) (string, error) {
	job, err := p.DB.ReadQ.GetJob(ctx, jobID)
	if err != nil {
		return "", err
	}
	return job.Status, nil
}

// JobLogLines returns a job's log lines as plain strings for assertions.
func (p *Panel) JobLogLines(ctx context.Context, jobID int64) ([]string, error) {
	rows, err := p.DB.ReadQ.ListJobLogs(ctx, store.ListJobLogsParams{JobID: jobID, Seq: -1})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Line)
	}
	return out, nil
}
