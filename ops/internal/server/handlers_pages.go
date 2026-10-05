package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/web/components"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	ctx := r.Context()
	d := components.DashboardData{User: user}
	d.Servers, _ = s.db.ReadQ.CountServers(ctx)
	d.ServersOnline, _ = s.db.ReadQ.CountServersOnline(ctx)
	d.Sites, _ = s.db.ReadQ.CountSites(ctx)
	d.WebApps, _ = s.db.ReadQ.CountWebApps(ctx)
	d.RunningJobs, _ = s.db.ReadQ.CountRunningJobs(ctx)

	// Warnings.
	d.ServersOffline, _ = s.db.ReadQ.CountServersOffline(ctx)
	cutoff := time.Now().Add(14 * 24 * time.Hour).Unix()
	d.CertsExpiring, _ = s.db.ReadQ.CountExpiringCerts(ctx, store.CountExpiringCertsParams{
		SslExpiresAt: nullInt(cutoff), SslExpiresAt_2: nullInt(cutoff),
	})
	d.DisksLow = s.countLowDisk(ctx)
	render(w, r, components.Dashboard(d))
}

// countLowDisk reads each server's facts_json and counts disk_used_pct > 80.
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

func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	rows, err := s.db.ReadQ.ListAudit(r.Context(), listAuditDefault())
	if err != nil {
		http.Error(w, "could not load audit log", http.StatusInternalServerError)
		return
	}
	render(w, r, components.AuditLog(user, rows))
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
