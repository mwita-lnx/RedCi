package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// discoveredBench mirrors the agent's discovery/status result shape.
type discoveredBench struct {
	Path        string             `json:"path"`
	Apps        []string           `json:"apps"`
	AppVersions map[string]string  `json:"app_versions,omitempty"`
	AppGit      map[string]appGit  `json:"app_git,omitempty"`
	Facts       json.RawMessage    `json:"facts,omitempty"`
	Sites       []struct {
		Domain        string   `json:"domain"`
		HasSSL        bool     `json:"has_ssl"`
		InstalledApps []string `json:"installed_apps"`
	} `json:"sites"`
}

// appGit mirrors the agent's AppGit (ops.AppGit).
type appGit struct {
	Branch    string `json:"branch,omitempty"`
	Commit    string `json:"commit,omitempty"`
	RemoteURL string `json:"remote_url,omitempty"`
}

type statusResult struct {
	NginxVersion string            `json:"nginx_version"`
	BenchVersion string            `json:"bench_version"`
	Benches      []discoveredBench `json:"benches"`
}

// ImportDiscovered re-imports the most recent discovery payload the agent pushed
// for a server (POST /agent/v1/discovery). This backs the on-demand "Refresh"
// action; routine updates happen automatically on each push. Returns an error if
// the agent has not pushed anything yet. Nothing on the server changes.
func (s *Server) ImportDiscovered(ctx context.Context, serverID int64) (benches, sites int, err error) {
	row, err := s.db.ReadQ.GetServerDiscovery(ctx, serverID)
	if err != nil || !row.Valid || row.String == "" {
		return 0, 0, fmt.Errorf("no discovery from this server's agent yet; is the agent online?")
	}
	return s.ImportDiscoveredResult(ctx, serverID, json.RawMessage(row.String))
}

// ImportDiscoveredResult imports a discovery payload directly, without reading
// it from a job result. It is the shared core used both by the server_status
// job-success path (via ImportDiscovered) and the agent's periodic discovery
// push (POST /agent/v1/discovery), so the same reconciliation logic runs either
// way. Nothing on the server changes.
func (s *Server) ImportDiscoveredResult(ctx context.Context, serverID int64, raw json.RawMessage) (benches, sites int, err error) {
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
		}
		// Ensure app rows exist (also for benches imported earlier without them).
		for _, app := range db.Apps {
			_, _ = s.db.WriteQ.CreateFrappeApp(ctx, store.CreateFrappeAppParams{
				BenchID: benchID, AppName: app,
			})
		}
		// Store the bench's runtime facts + frappe version. The file-only
		// discovery push does not probe `bench version`, so BenchVersion may be
		// empty — in that case preserve whatever version a prior server_status
		// job recorded rather than wiping it.
		factsJSON := "{}"
		if len(db.Facts) > 0 {
			factsJSON = string(db.Facts)
		}
		frappeVer := res.BenchVersion
		if frappeVer == "" {
			if cur, err := s.db.ReadQ.GetBench(ctx, benchID); err == nil && cur.FrappeVersion.Valid {
				frappeVer = cur.FrappeVersion.String
			}
		}
		_ = s.db.WriteQ.SetBenchFacts(ctx, store.SetBenchFactsParams{
			FactsJson: factsJSON, FrappeVersion: nullStr(frappeVer), ID: benchID,
		})
		// Update app versions from what the agent read off disk.
		for app, ver := range db.AppVersions {
			if ver != "" {
				_ = s.db.WriteQ.SetFrappeAppCommit(ctx, store.SetFrappeAppCommitParams{
					BenchID: benchID, AppName: app, CurrentCommit: nullStr(ver),
				})
			}
		}
		// Store per-app git state (branch, HEAD commit, remote) read off .git.
		for app, g := range db.AppGit {
			_ = s.db.WriteQ.SetFrappeAppGit(ctx, store.SetFrappeAppGitParams{
				GitCommit: nullStr(g.Commit),
				GitBranch: nullStr(g.Branch),
				GitRemote: nullStr(g.RemoteURL),
				BenchID:   benchID,
				AppName:   app,
			})
		}
		// Import sites not already present; flip creating→active for existing ones.
		for _, site := range db.Sites {
			if existing, err := s.db.ReadQ.GetSiteByDomain(ctx, site.Domain); err == nil {
				// Always sync the installed app list from the agent.
				if len(site.InstalledApps) > 0 {
					appsJSON, _ := json.Marshal(site.InstalledApps)
					_ = s.db.WriteQ.UpdateSiteApps(ctx, store.UpdateSiteAppsParams{
						AppsJson: string(appsJSON), ID: existing.ID,
					})
				}
				// Promote creating→active once the agent confirms the site exists.
				if existing.Status == "creating" {
					_ = s.db.WriteQ.SetSiteStatus(ctx, store.SetSiteStatusParams{
						Status: "active", ID: existing.ID,
					})
				}
				continue
			}
			appsJSON := "[]"
			if len(site.InstalledApps) > 0 {
				b, _ := json.Marshal(site.InstalledApps)
				appsJSON = string(b)
			}
			if _, err := s.db.WriteQ.CreateSite(ctx, store.CreateSiteParams{
				BenchID:    benchID,
				Domain:     site.Domain,
				Status:     "active",
				AppsJson:   appsJSON,
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

func benchName(path string) string {
	// Use the last path element as a human name, e.g. frappe-bench.
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
