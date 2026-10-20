package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// discoveredWebApp mirrors the agent's ops.DiscoveredWebApp.
type discoveredWebApp struct {
	Name         string `json:"name"`
	Runtime      string `json:"runtime"`
	InternalPort int    `json:"internal_port"`
	Git          appGit `json:"git"`
}

// discoveredNginxVhost mirrors the agent's ops.DiscoveredNginxVhost.
type discoveredNginxVhost struct {
	Path         string   `json:"path"`
	ServerNames  []string `json:"server_names"`
	Content      string   `json:"content"`
	SHA256       string   `json:"sha256"`
	SSLExpiresAt int64    `json:"ssl_expires_at"`
}

type statusResult struct {
	NginxVersion string                 `json:"nginx_version"`
	BenchVersion string                 `json:"bench_version"`
	Benches      []discoveredBench      `json:"benches"`
	WebApps      []discoveredWebApp     `json:"web_apps,omitempty"`
	NginxVhosts  []discoveredNginxVhost `json:"nginx_vhosts,omitempty"`
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
	webapps := s.reconcileWebApps(ctx, serverID, res.WebApps)
	routes := s.reconcileNginxVhosts(ctx, serverID, res.NginxVhosts)
	s.auditSystemCtx(ctx, "server.import", "server", serverID,
		fmt.Sprintf("%d benches, %d sites, %d web apps, %d routes", benches, sites, webapps, routes))
	return benches, sites, nil
}

// reconcileNginxVhosts records every existing nginx vhost the agent discovered
// (sites-enabled / conf.d) as an UNMANAGED proxy_route: the panel shows it
// read-only with its verbatim config and never re-renders or overwrites it.
// Idempotent by (server_id, nginx_path): new files are inserted, changed files
// have their raw_config/domain refreshed, unchanged files are left alone. Only
// ever creates/updates managed=0 rows, so it can never disturb a route the
// operator created through RedCi. Returns the number of newly recorded vhosts.
func (s *Server) reconcileNginxVhosts(ctx context.Context, serverID int64, vhosts []discoveredNginxVhost) int {
	if len(vhosts) == 0 {
		return 0
	}
	imported := 0
	for _, v := range vhosts {
		domain := routeDomain(v)
		ssl := boolToInt(strings.Contains(v.Content, "ssl_certificate"))
		expiry := nullInt(0)
		if v.SSLExpiresAt > 0 {
			expiry = nullInt(v.SSLExpiresAt)
		}
		existing, err := s.db.ReadQ.GetRouteByServerPath(ctx, store.GetRouteByServerPathParams{
			ServerID: serverID, NginxPath: nullStr(v.Path),
		})
		if err == nil {
			// Already recorded; refresh verbatim config/domain/expiry if it drifted.
			// Guard: never touch a route that is actually managed.
			if existing.Managed == 0 && (nullStrVal(existing.RawConfig) != v.Content ||
				existing.Domain != domain || existing.SslExpiresAt.Int64 != v.SSLExpiresAt) {
				_ = s.db.WriteQ.UpdateDiscoveredRoute(ctx, store.UpdateDiscoveredRouteParams{
					Domain: domain, RawConfig: nullStr(v.Content), SslEnabled: ssl,
					SslExpiresAt: expiry, ID: existing.ID,
				})
			}
			continue
		}
		if _, err := s.db.WriteQ.CreateDiscoveredRoute(ctx, store.CreateDiscoveredRouteParams{
			ServerID:     serverID,
			Domain:       domain,
			Upstream:     v.Path, // reference only; unmanaged routes have no RedCi upstream
			SslEnabled:   ssl,
			SslExpiresAt: expiry,
			RawConfig:    nullStr(v.Content),
			NginxPath:    nullStr(v.Path),
		}); err != nil {
			s.log.Warn("import discovered nginx vhost failed", "server", serverID, "path", v.Path, "err", err)
			continue
		}
		imported++
	}
	return imported
}

// routeDomain picks a human domain for a discovered vhost: its first real
// server_name, else the config filename (for map-driven vhosts with only
// variable server_names). The (server_id, domain) UNIQUE constraint means a
// unique fallback matters, so the full file path keeps it distinct.
func routeDomain(v discoveredNginxVhost) string {
	if len(v.ServerNames) > 0 {
		return v.ServerNames[0]
	}
	base := v.Path
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' {
			return base[i+1:]
		}
	}
	return base
}

// reconcileWebApps creates a web_apps row for each app the agent discovered under
// the websites root that the panel does not already track, so operators do not
// have to re-declare apps that are already deployed. It is idempotent: an app is
// matched by name, and existing rows are left untouched (the operator still owns
// fields like health_path, which they fill in after import). Discovered apps are
// recorded with status='running' since the runtime file proves they are live.
// Returns the number of newly imported apps.
func (s *Server) reconcileWebApps(ctx context.Context, serverID int64, apps []discoveredWebApp) int {
	if len(apps) == 0 {
		return 0
	}
	imported := 0
	for _, a := range apps {
		if a.Name == "" || a.InternalPort == 0 {
			// No parseable port means we cannot satisfy the NOT NULL /
			// (server_id, internal_port) UNIQUE constraints; skip rather than guess.
			continue
		}
		if _, err := s.db.ReadQ.GetWebAppByName(ctx, a.Name); err == nil {
			continue // already tracked; operator owns its fields
		}
		runtime := a.Runtime
		if runtime != "docker" && runtime != "pm2" {
			runtime = "docker"
		}
		if _, err := s.db.WriteQ.CreateDiscoveredWebApp(ctx, store.CreateDiscoveredWebAppParams{
			ServerID:      serverID,
			Name:          a.Name,
			Runtime:       runtime,
			Repo:          a.Git.RemoteURL,
			Branch:        orDefault(a.Git.Branch, "main"),
			InternalPort:  int64(a.InternalPort),
			CurrentCommit: nullStr(a.Git.Commit),
		}); err != nil {
			s.log.Warn("import discovered web app failed", "server", serverID, "app", a.Name, "err", err)
			continue
		}
		imported++
	}
	return imported
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
