package server

import (
	"context"
	"net/http"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// toJobType converts the stored jobs.type string to the shared jobs.Type.
func toJobType(t string) jobs.Type { return jobs.Type(t) }

// jobSpec returns the default timeout (seconds) for a job type, defaulting to
// 300s for an unknown type.
func jobSpec(t string) (int, bool) {
	spec, ok := jobs.Lookup(jobs.Type(t))
	if !ok {
		return 300, false
	}
	return int(spec.Timeout.Seconds()), true
}

// auditSystem writes an audit row for a non-user actor (agent/system/webhook).
func (s *Server) auditSystem(r *http.Request, action, targetType string, targetID int64, detail string) {
	params := store.InsertAuditParams{
		Actor:  "system",
		Action: action,
		Ip:     nullStr(clientIP(r)),
	}
	if targetType != "" {
		params.TargetType = nullStr(targetType)
		params.TargetID = nullInt(targetID)
	}
	if detail != "" {
		params.DetailJson = nullStr(detail)
	}
	if err := s.db.WriteQ.InsertAudit(r.Context(), params); err != nil {
		s.log.Error("audit write failed", "action", action, "err", err)
	}
}

// auditSystemCtx is auditSystem without a request (background callers).
func (s *Server) auditSystemCtx(ctx context.Context, action, targetType string, targetID int64, detail string) {
	params := store.InsertAuditParams{Actor: "system", Action: action}
	if targetType != "" {
		params.TargetType = nullStr(targetType)
		params.TargetID = nullInt(targetID)
	}
	if detail != "" {
		params.DetailJson = nullStr(detail)
	}
	if err := s.db.WriteQ.InsertAudit(ctx, params); err != nil {
		s.log.Error("audit write failed", "action", action, "err", err)
	}
}
