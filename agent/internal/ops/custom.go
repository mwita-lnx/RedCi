package ops

import (
	"context"
	"encoding/json"

	"github.com/mwita-lnx/RedCi/agent/internal/nginx"
	"github.com/mwita-lnx/RedCi/agent/internal/runner"
	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// Env carries the agent-side dependencies a custom op needs.
type Env struct {
	Runner     *runner.Runner
	Nginx      *nginx.Manager
	AppsRoot   string
	BenchRoots []string
	NginxVer   string
}

// Log is the sink custom ops write to (same shape as the runner's sink).
type Log func(stream, line string)

// IsCustom reports whether a job type is handled by a custom op (control flow)
// rather than the generic step runner.
func IsCustom(t jobs.Type) bool {
	switch t {
	case jobs.TypeServerStatus, jobs.TypeListNginxConfigs,
		jobs.TypeWriteProxyRoute, jobs.TypeDeleteProxyRoute,
		jobs.TypeDeployWebApp, jobs.TypeRollbackWebApp:
		return true
	}
	return false
}

// Result is a custom op's outcome: a terminal status plus optional result JSON
// that the panel stores (cert expiry, new commit, discovered benches, ...).
type Result struct {
	Status   string // succeeded | failed
	ExitCode int
	Error    string
	Result   json.RawMessage
}

func ok(result any) Result {
	raw, _ := json.Marshal(result)
	return Result{Status: "succeeded", Result: raw}
}

func fail(err error) Result {
	return Result{Status: "failed", ExitCode: -1, Error: err.Error()}
}

// RunCustom dispatches a custom-op job. The caller has already validated params.
func RunCustom(ctx context.Context, env Env, job protocol.Job, log Log) Result {
	switch job.Type {
	case jobs.TypeServerStatus:
		return runServerStatus(ctx, env, log)
	case jobs.TypeListNginxConfigs:
		return runListNginx(env, log)
	case jobs.TypeWriteProxyRoute:
		return runWriteRoute(ctx, env, job, log)
	case jobs.TypeDeleteProxyRoute:
		return runDeleteRoute(ctx, env, job, log)
	case jobs.TypeDeployWebApp:
		return runDeployWebApp(ctx, env, job, log)
	case jobs.TypeRollbackWebApp:
		return runRollbackWebApp(ctx, env, job, log)
	default:
		return fail(errUnsupported(job.Type))
	}
}

func errUnsupported(t jobs.Type) error {
	return &opError{"unsupported custom op: " + string(t)}
}

type opError struct{ msg string }

func (e *opError) Error() string { return e.msg }
