package agentcfg

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/mwita-lnx/RedCi/agent/internal/client"
	"github.com/mwita-lnx/RedCi/agent/internal/journal"
	"github.com/mwita-lnx/RedCi/agent/internal/nginx"
	"github.com/mwita-lnx/RedCi/agent/internal/ops"
	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// Agent runs the heartbeat/claim/run/report loop for one server.
type Agent struct {
	cfg     Config
	cred    Credential
	cli     *client.Client
	jrnl    *journal.Journal
	run      *runner.Runner
	nginx    *nginx.Manager
	log      *slog.Logger
	version  string
	nginxVer string // detected at startup for http2 directive choice

	mu        sync.Mutex
	runningID int64 // job currently executing, for heartbeat lease renewal
}

// NewAgent builds an Agent. jrnl may be nil in dry-run with no credential.
func NewAgent(cfg Config, cred Credential, cli *client.Client, jrnl *journal.Journal, run *runner.Runner, version string, log *slog.Logger) *Agent {
	return &Agent{
		cfg: cfg, cred: cred, cli: cli, jrnl: jrnl, run: run,
		nginx:   nginx.New(run, "", ""),
		version: version, log: log,
	}
}

// Run drives the loop until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	// Detect the nginx version once so route templates use the right http2
	// directive. Best-effort; empty means "assume legacy listen ... http2".
	a.nginxVer = ops.DetectNginxVersion(ctx, a.run)

	// Recover any job interrupted by a crash/reboot: report it failed.
	a.recoverInterrupted(ctx)
	a.flushOutbox(ctx)

	go a.heartbeatLoop(ctx)

	bo := client.NewBackoff(time.Second, 30*time.Second)
	for {
		if ctx.Err() != nil {
			return nil
		}
		job, ok, err := a.cli.NextJob(ctx)
		if err != nil {
			d := bo.Next()
			a.log.Warn("jobs/next failed; backing off", "err", err, "delay", d.String())
			if !sleep(ctx, d) {
				return nil
			}
			continue
		}
		bo.Reset()
		if !ok {
			continue // 204: poll again immediately
		}
		a.handle(ctx, job)
	}
}

func (a *Agent) setRunning(id int64) {
	a.mu.Lock()
	a.runningID = id
	a.mu.Unlock()
}

// handle validates, journals, runs and reports one job. Straightforward jobs
// use the generic step runner; control-flow jobs (routes, web apps, status)
// use a custom op.
func (a *Agent) handle(ctx context.Context, job protocol.Job) {
	a.log.Info("claimed job", "id", job.ID, "type", job.Type)
	a.setRunning(job.ID)
	defer a.setRunning(0)

	paramsJSON, _ := json.Marshal(job.Params)
	if a.jrnl != nil {
		_ = a.jrnl.Begin(ctx, job.ID, string(job.Type), string(paramsJSON))
	}
	if err := a.cli.Start(ctx, job.ID); err != nil {
		a.log.Warn("start failed", "id", job.ID, "err", err)
	}
	sink := newLogSink(ctx, a.cli, a.jrnl, job.ID)

	var result protocol.ResultRequest
	if ops.IsCustom(job.Type) {
		result = a.runCustom(ctx, job, sink)
	} else {
		result = a.runSteps(ctx, job, sink)
	}
	sink.flush()

	if a.jrnl != nil {
		st := journal.StatusSucceeded
		if result.Status != "succeeded" {
			st = journal.StatusFailed
		}
		_ = a.jrnl.Finish(ctx, job.ID, st)
	}
	a.report(ctx, job.ID, result)
}

// runSteps executes a job via the generic step plan.
func (a *Agent) runSteps(ctx context.Context, job protocol.Job, sink *logSink) protocol.ResultRequest {
	steps, err := ops.Plan(job, ops.PlanInput{BenchRoots: a.cfg.BenchRoots, NginxVersion: a.nginxVer})
	if err != nil {
		a.log.Warn("job validation failed", "id", job.ID, "err", err)
		return protocol.ResultRequest{Status: "failed", ExitCode: -1, Error: err.Error()}
	}
	redacts := ops.RedactionValues(job)
	for i := range steps {
		steps[i].Redact = append(steps[i].Redact, redacts...)
	}
	timeout := time.Duration(job.TimeoutSeconds) * time.Second
	res := a.run.Run(ctx, steps, timeout, sink.emit, func(name string) {
		if a.jrnl != nil {
			_ = a.jrnl.SetStep(ctx, job.ID, name)
		}
	})
	out := protocol.ResultRequest{ExitCode: res.ExitCode, Status: "succeeded"}
	if res.Err != nil {
		out.Status = "failed"
		out.Error = res.Err.Error()
	}
	return out
}

// runCustom validates then dispatches a control-flow op.
func (a *Agent) runCustom(ctx context.Context, job protocol.Job, sink *logSink) protocol.ResultRequest {
	// Validate the params (the generic path validates inside Plan; custom ops
	// validate here so an invalid job never touches the host).
	if _, err := jobs.ParseParams(job.Type, job.Params, a.cfg.BenchRoots); err != nil {
		return protocol.ResultRequest{Status: "failed", ExitCode: -1, Error: err.Error()}
	}
	r := ops.RunCustom(ctx, ops.Env{
		Runner:     a.run,
		Nginx:      a.nginx,
		AppsRoot:   a.cfg.AppsRoot,
		BenchRoots: a.cfg.BenchRoots,
		NginxVer:   a.nginxVer,
	}, job, sink.emit)
	return protocol.ResultRequest{
		Status: r.Status, ExitCode: r.ExitCode, Error: r.Error, Result: r.Result,
	}
}

// report delivers the result, queuing it in the outbox if the panel is down.
func (a *Agent) report(ctx context.Context, jobID int64, res protocol.ResultRequest) {
	if err := a.cli.SendResult(ctx, jobID, res); err != nil {
		a.log.Warn("result delivery failed; queued in outbox", "id", jobID, "err", err)
		if a.jrnl != nil {
			payload, _ := json.Marshal(res)
			_ = a.jrnl.Enqueue(ctx, jobID, "result", string(payload))
		}
		return
	}
	if a.jrnl != nil {
		_ = a.jrnl.MarkReported(ctx, jobID)
	}
}

func (a *Agent) recoverInterrupted(ctx context.Context) {
	if a.jrnl == nil {
		return
	}
	items, err := a.jrnl.FindInterrupted(ctx)
	if err != nil {
		return
	}
	for _, it := range items {
		a.log.Warn("recovering interrupted job", "id", it.JobID, "step", it.Step)
		_ = a.jrnl.Finish(ctx, it.JobID, journal.StatusFailed)
		a.report(ctx, it.JobID, protocol.ResultRequest{
			Status: "failed", ExitCode: -1,
			Error: "agent restarted during step " + it.Step,
		})
	}
}

// flushOutbox delivers any queued payloads from a prior panel outage.
func (a *Agent) flushOutbox(ctx context.Context) {
	if a.jrnl == nil {
		return
	}
	items, err := a.jrnl.PeekOutbox(ctx, 100)
	if err != nil {
		return
	}
	for _, it := range items {
		switch it.Kind {
		case "logs":
			var req protocol.LogsRequest
			if json.Unmarshal([]byte(it.Payload), &req) == nil {
				if a.cli.SendLogs(ctx, it.JobID, req.Lines) == nil {
					_ = a.jrnl.DeleteOutbox(ctx, it.ID)
				}
			}
		case "result":
			var req protocol.ResultRequest
			if json.Unmarshal([]byte(it.Payload), &req) == nil {
				if a.cli.SendResult(ctx, it.JobID, req) == nil {
					_ = a.jrnl.MarkReported(ctx, it.JobID)
					_ = a.jrnl.DeleteOutbox(ctx, it.ID)
				}
			}
		}
	}
}

func (a *Agent) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.mu.Lock()
			running := a.runningID
			a.mu.Unlock()
			_, err := a.cli.Heartbeat(ctx, protocol.HeartbeatRequest{
				AgentVersion: a.version,
				Facts:        a.cfg.CollectFacts(),
				RunningJobID: running,
			})
			if err != nil {
				a.log.Debug("heartbeat failed", "err", err)
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
