package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/protocol"
)

// agentCtxKey carries the authenticated server on the request context.
type agentCtxKeyT int

const agentCtxKey agentCtxKeyT = 0

// requireAgent authenticates an agent by its bearer token and checks the
// protocol version. The authenticated server is placed on the context.
func (s *Server) requireAgent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get(protocol.HeaderProtocol); v != "" && v != strconv.Itoa(protocol.Version) {
			http.Error(w, "upgrade agent: unsupported protocol version", http.StatusUpgradeRequired)
			return
		}
		token := bearerToken(r)
		if token == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		srv, err := s.db.ReadQ.FindServerByAgentTokenHash(r.Context(), nullStr(hashToken(token)))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), agentCtxKey, srv)
		next(w, r.WithContext(ctx))
	}
}

func agentFrom(ctx context.Context) (store.Server, bool) {
	srv, ok := ctx.Value(agentCtxKey).(store.Server)
	return srv, ok
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return ""
}

// handleAgentRegister completes enrollment: it checks the one-time token hash
// and expiry, mints the long-lived agent token, and returns {server_id, token}.
func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req protocol.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.EnrollmentToken == "" {
		http.Error(w, "missing enrollment token", http.StatusBadRequest)
		return
	}
	srv, err := s.db.ReadQ.FindServerByEnrollmentHash(r.Context(),
		store.FindServerByEnrollmentHashParams{
			EnrollmentTokenHash:  nullStr(hashToken(req.EnrollmentToken)),
			EnrollmentExpiresAt:  nullInt(time.Now().Unix()),
		})
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "invalid or expired enrollment token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	token, hash := newToken()
	factsJSON, _ := json.Marshal(req.Facts)
	if err := s.db.WriteQ.CompleteEnrollment(r.Context(), store.CompleteEnrollmentParams{
		AgentTokenHash: nullStr(hash),
		AgentVersion:   nullStr(req.AgentVersion),
		Hostname:       req.Hostname,
		FactsJson:      nullStr(string(factsJSON)),
		ID:             srv.ID,
	}); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.auditSystem(r, "server.enrolled", "server", srv.ID, req.Hostname)
	writeJSON(w, http.StatusOK, protocol.RegisterResponse{ServerID: srv.ID, Token: token})
}

// handleAgentHeartbeat records a heartbeat, renews the running job's lease, and
// returns any jobs the panel wants cancelled.
func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	srv, _ := agentFrom(r.Context())
	var req protocol.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	factsJSON, _ := json.Marshal(req.Facts)
	_ = s.db.WriteQ.Heartbeat(r.Context(), store.HeartbeatParams{
		AgentVersion: nullStr(req.AgentVersion),
		FactsJson:    nullStr(string(factsJSON)),
		ID:           srv.ID,
	})
	if req.RunningJobID != 0 {
		_ = s.db.WriteQ.RenewLease(r.Context(), req.RunningJobID)
	}
	cancelIDs, _ := s.db.ReadQ.ListCancelRequestedForServer(r.Context(), srv.ID)
	writeJSON(w, http.StatusOK, protocol.HeartbeatResponse{Cancel: cancelIDs})
}

// handleAgentJobsNext claims the next ready job for this server, long-polling
// up to 30s for one to appear before returning 204.
func (s *Server) handleAgentJobsNext(w http.ResponseWriter, r *http.Request) {
	srv, _ := agentFrom(r.Context())

	if job, ok := s.tryClaim(r.Context(), srv.ID); ok {
		writeJSON(w, http.StatusOK, job)
		return
	}
	// Wait for a wake signal or 30s, then try once more.
	select {
	case <-s.notifyChan(srv.ID):
	case <-time.After(30 * time.Second):
	case <-r.Context().Done():
		return
	}
	if job, ok := s.tryClaim(r.Context(), srv.ID); ok {
		writeJSON(w, http.StatusOK, job)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tryClaim atomically claims the next ready job and resolves its secrets.
func (s *Server) tryClaim(ctx context.Context, serverID int64) (protocol.Job, bool) {
	row, err := s.db.WriteQ.ClaimNextJob(ctx, serverID)
	if err != nil {
		return protocol.Job{}, false
	}
	resolved, err := s.resolveSecrets(ctx, []byte(row.ParamsJson))
	if err != nil {
		// Could not resolve: fail the job immediately so it is not retried blindly.
		_ = s.db.WriteQ.FinishJob(ctx, store.FinishJobParams{
			Status: "failed", Error: nullStr("secret resolution failed: " + err.Error()), ID: row.ID,
		})
		s.publishStatus(row.ID, "failed")
		return protocol.Job{}, false
	}
	spec, _ := jobSpec(row.Type)
	return protocol.Job{
		ID:             row.ID,
		Type:           toJobType(row.Type),
		TimeoutSeconds: spec,
		Params:         resolved,
	}, true
}

// handleAgentJobStart marks a claimed job running.
func (s *Server) handleAgentJobStart(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := s.db.WriteQ.StartJob(r.Context(), id); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.publishStatus(id, "running")
	w.WriteHeader(http.StatusOK)
}

// handleAgentJobLogs stores a batch of log lines (idempotent on seq) and
// publishes each to the event bus for live SSE.
func (s *Server) handleAgentJobLogs(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req protocol.LogsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(req.Lines) > protocol.MaxLogBatch {
		http.Error(w, "too many lines", http.StatusBadRequest)
		return
	}
	for _, ln := range req.Lines {
		_ = s.db.WriteQ.InsertJobLog(r.Context(), store.InsertJobLogParams{
			JobID: id, Seq: ln.Seq, Ts: ln.TS, Stream: ln.Stream, Line: ln.Line,
		})
		s.bus.Publish(events.Event{
			JobID: id, Kind: events.KindLog, Seq: ln.Seq, Stream: ln.Stream, Line: ln.Line,
		})
	}
	w.WriteHeader(http.StatusOK)
}

// handleAgentJobResult records the final status.
func (s *Server) handleAgentJobResult(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	var req protocol.ResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	job, err := s.db.ReadQ.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, "unknown job", http.StatusNotFound)
		return
	}
	// A late result for a job already marked lost is recorded but does not
	// change the state (a human reviews it).
	if job.Status == "lost" {
		s.bus.Publish(events.Event{JobID: id, Kind: events.KindLog, Stream: "system",
			Line: "late result received after lease expiry: " + req.Status})
		w.WriteHeader(http.StatusOK)
		return
	}
	status := "succeeded"
	if req.Status != "succeeded" {
		status = "failed"
	}
	_ = s.db.WriteQ.FinishJob(r.Context(), store.FinishJobParams{
		Status:     status,
		ExitCode:   nullInt(int64(req.ExitCode)),
		Error:      nullStr(req.Error),
		ResultJson: nullStr(string(req.Result)),
		ID:         id,
	})
	s.publishStatus(id, status)
	s.bus.Publish(events.Event{JobID: id, Kind: events.KindDone})
	w.WriteHeader(http.StatusOK)
}

func (s *Server) publishStatus(jobID int64, status string) {
	s.bus.Publish(events.Event{JobID: jobID, Kind: events.KindStatus, Status: status})
}

// --- small helpers ---

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
