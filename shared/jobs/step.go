package jobs

import "time"

// Step is one command the agent runs for a job. Args are always a list — never
// a shell string. Secrets live in Args as resolved values at run time and are
// redacted before any logging; Redact lists the arg indices to mask in logs.
type Step struct {
	Name      string        // shown in the UI as "== step: <name>"
	Dir       string        // working directory; empty = agent default
	Command   string        // absolute path or PATH-resolved binary
	Args      []string      // arguments, never a shell line
	Env       []string      // extra KEY=VALUE pairs for this step only
	Timeout   time.Duration // per-step timeout; 0 = use the job timeout
	Redact    []string      // substrings to replace with *** in logs
	AlwaysRun bool          // run even after an earlier step failed (deferred cleanup)
}

// Planner is implemented by job params that can produce a concrete step plan.
// The agent's runner executes the returned steps in order. Keeping the planner
// in the shared package lets golden tests assert the exact argv in CI.
type Planner interface {
	// Steps returns the ordered commands for this job. ctx carries values the
	// plan needs that are not in the params (e.g. the resolved site name).
	Steps(ctx PlanContext) ([]Step, error)
}

// PlanContext carries agent-side facts a plan may need, such as the nginx
// version (for http2 syntax) and resolved secret values.
type PlanContext struct {
	NginxVersion string
	// Secrets maps a secret reference to its resolved plaintext value.
	Secrets map[SecretRef]string
}

// secret resolves a reference to its plaintext, or returns the input unchanged
// if it was never a reference (already resolved).
func (c PlanContext) secret(ref SecretRef) string {
	if v, ok := c.Secrets[ref]; ok {
		return v
	}
	return string(ref)
}
