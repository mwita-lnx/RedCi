package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// discoveredBench mirrors the agent's server_status result shape.
type discoveredBench struct {
	Path  string `json:"path"`
	Apps  []string `json:"apps"`
	Sites []struct {
		Domain string `json:"domain"`
		HasSSL bool   `json:"has_ssl"`
	} `json:"sites"`
}

type statusResult struct {
	NginxVersion string            `json:"nginx_version"`
	BenchVersion string            `json:"bench_version"`
	Benches      []discoveredBench `json:"benches"`
}

// RunServerStatus enqueues a server_status job for a server (used by "refresh"
// and after enrollment). Returns the deploy id.
func (s *Server) RunServerStatus(ctx context.Context, serverID int64, userID int64) (int64, error) {
	depID, err := s.createDeploy(ctx, "server_status", "server", serverID, "ui", "", userID, []jobSeed{
		{serverID: serverID, typ: jobs.TypeServerStatus, params: jobs.ServerStatusParams{}},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// ImportDiscovered reads the latest succeeded server_status result for a server
// and creates benches, sites and frappe_apps rows for anything not already
// known. Nothing on the server changes.
func (s *Server) ImportDiscovered(ctx context.Context, serverID int64) (benches, sites int, err error) {
	raw, err := s.latestStatusResult(ctx, serverID)
	if err != nil {
		return 0, 0, err
	}
	var res statusResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, 0, fmt.Errorf("parse status: %w", err)
	}

	existing, _ := s.db.ReadQ.ListBenchesForServer(ctx, serverID)
	known := map[string]int64{}
	for _, b := range existing {
		known[b.Path] = b.ID
	}

	for _, db := range res.Benches {
		benchID, ok := known[db.Path]
		if !ok {
			created, err := s.db.WriteQ.CreateBench(ctx, store.CreateBenchParams{
				ServerID:      serverID,
				Name:          benchName(db.Path),
				Path:          db.Path,
				FrappeVersion: nullStr(res.BenchVersion),
			})
			if err != nil {
				return benches, sites, err
			}
			benchID = created.ID
			benches++
			// Record its apps.
			for _, app := range db.Apps {
				_, _ = s.db.WriteQ.CreateFrappeApp(ctx, store.CreateFrappeAppParams{
					BenchID: benchID, AppName: app,
				})
			}
		}
		// Import sites not already present.
		for _, site := range db.Sites {
			if _, err := s.db.ReadQ.GetSiteByDomain(ctx, site.Domain); err == nil {
				continue // already known
			}
			if _, err := s.db.WriteQ.CreateSite(ctx, store.CreateSiteParams{
				BenchID:    benchID,
				Domain:     site.Domain,
				Status:     "active",
				AppsJson:   "[]",
				SslEnabled: boolToInt(site.HasSSL),
			}); err != nil {
				return benches, sites, err
			}
			sites++
		}
	}
	s.auditSystemCtx(ctx, "server.import", "server", serverID, fmt.Sprintf("%d benches, %d sites", benches, sites))
	return benches, sites, nil
}

// latestStatusResult returns the result_json of the most recent succeeded
// server_status job for a server.
func (s *Server) latestStatusResult(ctx context.Context, serverID int64) (json.RawMessage, error) {
	row, err := s.db.ReadQ.LatestResultForType(ctx, store.LatestResultForTypeParams{
		ServerID: serverID, Type: string(jobs.TypeServerStatus),
	})
	if err != nil {
		return nil, fmt.Errorf("no server_status result yet; run a refresh first")
	}
	if !row.Valid {
		return nil, fmt.Errorf("server_status result is empty")
	}
	return json.RawMessage(row.String), nil
}

func benchName(path string) string {
	// Use the last path element as a human name, e.g. frappe-bench.
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
