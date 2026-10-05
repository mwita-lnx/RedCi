// Package ops maps a job (type + resolved params) to the concrete step plan
// the runner executes. Validation and the step plans live in shared/jobs, so
// the panel and agent agree on both; this package only selects and builds.
package ops

import (
	"encoding/json"
	"fmt"

	"github.com/mwita-lnx/RedCi/shared/jobs"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// PlanInput carries agent-side facts a plan may need.
type PlanInput struct {
	NginxVersion string
	BenchRoots   []string
}

// Plan validates a job's resolved params and returns its step plan. Because the
// params arrive with secrets already resolved, the plan's secret map is empty
// and the resolved values flow straight through the params structs.
func Plan(job protocol.Job, in PlanInput) ([]jobs.Step, error) {
	p, err := jobs.ParseParams(job.Type, job.Params, in.BenchRoots)
	if err != nil {
		return nil, fmt.Errorf("validate %s: %w", job.Type, err)
	}
	planner, ok := p.(jobs.Planner)
	if !ok {
		return nil, fmt.Errorf("job type %s has no step plan", job.Type)
	}
	// Secrets are already resolved into the params, so pass an empty map; the
	// planner's ctx.secret() falls through to the stored (resolved) value.
	return planner.Steps(jobs.PlanContext{
		NginxVersion: in.NginxVersion,
		Secrets:      nil,
	})
}

// RedactionValues returns the resolved secret values in a job's params so the
// runner can mask them in logs even though they are no longer references.
func RedactionValues(job protocol.Job) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(job.Params, &obj); err != nil {
		return nil
	}
	var vals []string
	for k, v := range obj {
		if !isSecretField(k) {
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			vals = append(vals, s)
		}
	}
	return vals
}

// isSecretField names the params fields that carry secrets, so their resolved
// values are redacted from logs.
func isSecretField(name string) bool {
	switch name {
	case "admin_password", "db_root_password", "env", "token":
		return true
	}
	return false
}
