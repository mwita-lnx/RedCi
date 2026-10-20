package server

import (
	"context"
	"encoding/json"
	"net/http"
)

// countLowDisk reads each server's facts_json and counts disk_used_pct > 80.
// Used by the dashboard summary in the JSON API.
func (s *Server) countLowDisk(ctx context.Context) int64 {
	servers, err := s.db.ReadQ.ListServers(ctx)
	if err != nil {
		return 0
	}
	var n int64
	for _, sv := range servers {
		if !sv.FactsJson.Valid {
			continue
		}
		var facts struct {
			DiskUsedPct int `json:"disk_used_pct"`
		}
		if json.Unmarshal([]byte(sv.FactsJson.String), &facts) == nil && facts.DiskUsedPct > 80 {
			n++
		}
	}
	return n
}

// handleHealthz reports basic liveness for an external uptime check. It pings
// the database; the worker health check is added when the worker lands.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{"db": "ok"}
	code := http.StatusOK
	if err := s.db.Reader.PingContext(r.Context()); err != nil {
		status["db"] = "error"
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status)
}
