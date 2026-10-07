// Package protocol defines the request and response types for the agent<->panel
// HTTP API under /agent/v1/*. It is imported by both binaries so the wire shape
// is defined in exactly one place. All requests carry Authorization: Bearer
// <token> and X-Ops-Protocol: <Version>.
package protocol

import (
	"encoding/json"

	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// Version is the protocol version. The panel accepts this version and the one
// before it; an unknown version gets a 426 telling the operator to upgrade.
const Version = 1

// HeaderProtocol is the request header carrying the protocol version.
const HeaderProtocol = "X-Ops-Protocol"

// Facts describe a server, collected by the agent and sent at register and
// heartbeat. Stored by the panel as servers.facts_json.
type Facts struct {
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	CPUs          int      `json:"cpus"`
	MemTotalMB    int64    `json:"mem_total_mb"`
	DiskFreeGB    int64    `json:"disk_free_gb"`
	DiskUsedPct   int      `json:"disk_used_pct"`
	CPUUsedPct    int      `json:"cpu_used_pct"`    // live load, sampled each heartbeat
	MemUsedPct    int      `json:"mem_used_pct"`    // live memory pressure
	NginxVersion  string   `json:"nginx_version,omitempty"`
	BenchVersion  string   `json:"bench_version,omitempty"`
	DockerVersion string   `json:"docker_version,omitempty"`
	BenchRoots    []string `json:"bench_roots,omitempty"`
}

// RegisterRequest is POST /agent/v1/register.
type RegisterRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	Hostname        string `json:"hostname"`
	AgentVersion    string `json:"agent_version"`
	Facts           Facts  `json:"facts"`
}

// RegisterResponse returns the long-lived credential.
type RegisterResponse struct {
	ServerID int64  `json:"server_id"`
	Token    string `json:"token"`
}

// HeartbeatRequest is POST /agent/v1/heartbeat, sent every 15s.
type HeartbeatRequest struct {
	AgentVersion string `json:"agent_version"`
	Facts        Facts  `json:"facts"`
	RunningJobID int64  `json:"running_job_id,omitempty"` // 0 if idle
}

// HeartbeatResponse tells the agent which jobs to cancel.
type HeartbeatResponse struct {
	Cancel []int64 `json:"cancel"`
}

// Job is the unit returned by GET /agent/v1/jobs/next. Params has secret
// references already resolved to plaintext values (over TLS only).
type Job struct {
	ID             int64           `json:"id"`
	Type           jobs.Type       `json:"type"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Params         json.RawMessage `json:"params"`
}

// LogLine is one line of job output.
type LogLine struct {
	Seq    int64  `json:"seq"`
	TS     int64  `json:"ts"`
	Stream string `json:"stream"` // stdout | stderr | system
	Line   string `json:"line"`
}

// LogsRequest is POST /agent/v1/jobs/{id}/logs, up to 500 lines per call.
type LogsRequest struct {
	Lines []LogLine `json:"lines"`
}

// ResultRequest is POST /agent/v1/jobs/{id}/result, sent once at the end.
type ResultRequest struct {
	Status   string          `json:"status"` // succeeded | failed
	ExitCode int             `json:"exit_code"`
	Error    string          `json:"error,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
}

// MaxLogBatch is the largest number of log lines in one LogsRequest.
const MaxLogBatch = 500

// MaxLineBytes caps a single output line (spec: 4 KB).
const MaxLineBytes = 4096
