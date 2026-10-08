package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// This file exposes a JSON API under /api/v1 consumed by the React SPA. It
// reuses the same store queries and action methods as the server-rendered
// pages; handlers here only differ in that they encode JSON instead of HTML.

// writeJSONAPI encodes v as a JSON response.
func writeJSONAPI(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// apiError writes a JSON {error} body.
func apiError(w http.ResponseWriter, code int, msg string) {
	writeJSONAPI(w, code, map[string]string{"error": msg})
}

// decodeJSON reads a JSON body into v; returns false and writes 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// --- identity ---

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	writeJSONAPI(w, http.StatusOK, map[string]any{
		"id":    user.ID,
		"email": user.Email,
		"role":  user.Role,
	})
}

// --- dashboard ---

func (s *Server) apiDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	type dash struct {
		Servers        int64 `json:"servers"`
		ServersOnline  int64 `json:"servers_online"`
		ServersOffline int64 `json:"servers_offline"`
		Sites          int64 `json:"sites"`
		WebApps        int64 `json:"web_apps"`
		RunningJobs    int64 `json:"running_jobs"`
		CertsExpiring  int64 `json:"certs_expiring"`
		DisksLow       int64 `json:"disks_low"`
	}
	var d dash
	d.Servers, _ = s.db.ReadQ.CountServers(ctx)
	d.ServersOnline, _ = s.db.ReadQ.CountServersOnline(ctx)
	d.ServersOffline, _ = s.db.ReadQ.CountServersOffline(ctx)
	d.Sites, _ = s.db.ReadQ.CountSites(ctx)
	d.WebApps, _ = s.db.ReadQ.CountWebApps(ctx)
	d.RunningJobs, _ = s.db.ReadQ.CountRunningJobs(ctx)
	cutoff := time.Now().Add(14 * 24 * time.Hour).Unix()
	d.CertsExpiring, _ = s.db.ReadQ.CountExpiringCerts(ctx, store.CountExpiringCertsParams{
		SslExpiresAt: nullInt(cutoff), SslExpiresAt_2: nullInt(cutoff),
	})
	d.DisksLow = s.countLowDisk(ctx)
	writeJSONAPI(w, http.StatusOK, d)
}

// --- sites ---

func (s *Server) apiSites(w http.ResponseWriter, r *http.Request) {
	sites, _ := s.db.ReadQ.ListSites(r.Context())
	out := make([]any, 0, len(sites))
	for _, st := range sites {
		if st.Status == "archived" {
			continue
		}
		out = append(out, siteJSON(st))
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiSiteDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	site, err := s.db.ReadQ.GetSite(ctx, pathID(r))
	if err != nil {
		apiError(w, http.StatusNotFound, "site not found")
		return
	}
	var installedApps []string
	_ = json.Unmarshal([]byte(site.AppsJson), &installedApps)

	frappeApps, _ := s.db.ReadQ.ListFrappeAppsForBench(ctx, site.BenchID)
	verMap := map[string]string{}
	for _, fa := range frappeApps {
		if fa.CurrentCommit.Valid {
			verMap[fa.AppName] = fa.CurrentCommit.String
		}
	}
	type appInfo struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		Rollbackable bool `json:"rollbackable"`
	}
	apps := make([]appInfo, 0, len(installedApps))
	for _, name := range installedApps {
		rb := false
		if _, err := s.db.ReadQ.GetDeployPoint(ctx, store.GetDeployPointParams{
			BenchID: site.BenchID, AppName: name,
		}); err == nil {
			rb = true
		}
		apps = append(apps, appInfo{Name: name, Version: verMap[name], Rollbackable: rb})
	}
	resp := siteJSON(site)
	resp["apps"] = apps
	writeJSONAPI(w, http.StatusOK, resp)
}

func siteJSON(st store.Site) map[string]any {
	return map[string]any{
		"id":      st.ID,
		"domain":  st.Domain,
		"status":  st.Status,
		"ssl":     st.SslEnabled != 0,
		"bench_id": st.BenchID,
	}
}

func (s *Server) apiNewSite(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	var in struct {
		BenchID        int64    `json:"bench_id"`
		Domain         string   `json:"domain"`
		Apps           []string `json:"apps"`
		AdminPassword  string   `json:"admin_password"`
		DBRootUser     string   `json:"db_root_user"`
		DBRootPassword string   `json:"db_root_password"`
		LeEmail        string   `json:"le_email"`
		SSL            bool     `json:"ssl"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.DBRootUser == "" {
		in.DBRootUser = "root"
	}
	depID, err := s.CreateSiteDeploy(r.Context(), in.BenchID, strings.TrimSpace(in.Domain), in.Apps,
		in.AdminPassword, in.DBRootUser, in.DBRootPassword, strings.TrimSpace(in.LeEmail), in.SSL, user.ID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "site.create", "site", 0, in.Domain)
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"deploy_id": depID})
}

func (s *Server) apiSiteDelete(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := pathID(r)
	var in struct {
		DBRootUser     string `json:"db_root_user"`
		DBRootPassword string `json:"db_root_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.DBRootUser == "" {
		in.DBRootUser = "root"
	}
	depID, err := s.DeleteSite(r.Context(), id, in.DBRootUser, in.DBRootPassword, user.ID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "site.delete", "site", id, "")
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"deploy_id": depID})
}

func (s *Server) apiSiteBackup(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := pathID(r)
	var in struct {
		WithFiles bool `json:"with_files"`
	}
	_ = decodeJSON(w, r, &in) // body optional
	depID, err := s.BackupSite(r.Context(), id, in.WithFiles, user.ID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "site.backup", "site", id, "")
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"deploy_id": depID})
}

func (s *Server) apiSiteRefresh(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	site, err := s.db.ReadQ.GetSite(r.Context(), pathID(r))
	if err != nil {
		apiError(w, http.StatusNotFound, "site not found")
		return
	}
	bench, err := s.db.ReadQ.GetBench(r.Context(), site.BenchID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	depID, err := s.RunServerStatus(r.Context(), bench.ServerID, user.ID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "site.refresh", "site", site.ID, site.Domain)
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"deploy_id": depID})
}

func (s *Server) apiSiteRollback(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	site, err := s.db.ReadQ.GetSite(r.Context(), pathID(r))
	if err != nil {
		apiError(w, http.StatusNotFound, "site not found")
		return
	}
	app := r.PathValue("app")
	depID, err := s.RollbackFrappeApp(r.Context(), site.BenchID, app, user.ID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "app.rollback", "bench", site.BenchID, app)
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"deploy_id": depID})
}

// apiBenchesOverview powers the "Benches & sites" page: per-bench cards with
// runtime facts + app versions, the sites×apps version matrix, and backups.
func (s *Server) apiBenchesOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	benches, _ := s.db.ReadQ.ListBenches(ctx)
	servers, _ := s.db.ReadQ.ListServers(ctx)
	srvByID := map[int64]store.Server{}
	for _, sv := range servers {
		srvByID[sv.ID] = sv
	}

	// Pre-load all app sources so we can look up branch + latest_commit per app.
	allSources, _ := s.db.ReadQ.ListAppSources(ctx)
	srcByID := map[int64]store.AppSource{}
	for _, src := range allSources {
		srcByID[src.ID] = src
	}
	hasToken := s.ghToken(ctx) != ""
	// Cache latest commits to avoid repeated GitHub calls for the same repo+branch.
	type repoBranch struct{ repo, branch string }
	latestCache := map[repoBranch]string{}
	getLatest := func(repo, branch string) string {
		if !hasToken || !validRepo(repo) {
			return ""
		}
		key := repoBranch{repo, branch}
		if v, ok := latestCache[key]; ok {
			return v
		}
		v := s.ghLatestCommit(ctx, repo, branch)
		latestCache[key] = v
		return v
	}

	type matrixRow struct {
		SiteID       int64             `json:"site_id"`
		Site         string            `json:"site"`
		BenchID      int64             `json:"bench_id"`
		Bench        string            `json:"bench"`
		Env          string            `json:"env"`
		Status       string            `json:"status"`
		Backup       int64             `json:"last_backup"`
		Apps         []string          `json:"apps"`
		Versions     map[string]string `json:"versions"`
		Branches     map[string]string `json:"branches"`
		LatestCommits map[string]string `json:"latest_commits"`
	}
	appSet := map[string]bool{}
	var matrix []matrixRow
	benchOut := make([]any, 0, len(benches))
	backups := make([]any, 0)

	for _, b := range benches {
		sv := srvByID[b.ServerID]
		apps, _ := s.db.ReadQ.ListFrappeAppsForBench(ctx, b.ID)
		verByApp := map[string]string{}
		branchByApp := map[string]string{}
		latestByApp := map[string]string{}
		appNames := make([]string, 0, len(apps))
		for _, a := range apps {
			appNames = append(appNames, a.AppName)
			appSet[a.AppName] = true
			if a.CurrentCommit.Valid {
				verByApp[a.AppName] = a.CurrentCommit.String
			}
			// Look up the app source for branch + latest commit.
			if a.AppSourceID.Valid {
				if src, ok := srcByID[a.AppSourceID.Int64]; ok {
					branchByApp[a.AppName] = src.Branch
					latestByApp[a.AppName] = getLatest(src.Repo, src.Branch)
				}
			}
		}
		sitesForBench, _ := s.db.ReadQ.SitesForBench(ctx, b.ID)
		activeSites := 0
		for _, st := range sitesForBench {
			if st.Status == "archived" {
				continue
			}
			activeSites++
			// matrix row: this site's version of each app it has installed.
			var installed []string
			_ = json.Unmarshal([]byte(st.AppsJson), &installed)
			rowVers := map[string]string{}
			rowBranches := map[string]string{}
			rowLatest := map[string]string{}
			for _, app := range installed {
				rowVers[app] = verByApp[app] // bench-level version (per-site not tracked separately)
				rowBranches[app] = branchByApp[app]
				rowLatest[app] = latestByApp[app]
				appSet[app] = true
			}
			matrix = append(matrix, matrixRow{
				SiteID: st.ID, Site: st.Domain, BenchID: b.ID, Bench: b.Name, Env: benchEnvGuess(b.Name),
				Status: st.Status, Backup: st.LastBackupAt.Int64, Apps: installed, Versions: rowVers,
				Branches: rowBranches, LatestCommits: rowLatest,
			})
			if st.LastBackupAt.Valid {
				backups = append(backups, map[string]any{"site": st.Domain, "at": st.LastBackupAt.Int64})
			}
		}

		facts := map[string]any{}
		_ = json.Unmarshal([]byte(b.FactsJson), &facts)
		// Prefer the bench's own frappe app version over the server-level string.
		frappeVer := verByApp["frappe"]
		if frappeVer == "" {
			frappeVer = nullStrVal(b.FrappeVersion)
		}
		benchOut = append(benchOut, map[string]any{
			"id": b.ID, "name": b.Name, "path": b.Path,
			"server_id": b.ServerID, "server_name": sv.Name,
			"server_status": sv.Status,
			"frappe_version": frappeVer,
			"env":   benchEnvGuess(b.Name),
			"sites": activeSites,
			"apps":  appVersionsListFull(appNames, verByApp, branchByApp, latestByApp),
			"facts": facts,
		})
	}

	// stable sorted app columns for the matrix.
	cols := make([]string, 0, len(appSet))
	for a := range appSet {
		cols = append(cols, a)
	}
	sort.Strings(cols)
	// put frappe/erpnext first for readability.
	sort.SliceStable(cols, func(i, j int) bool { return appRank(cols[i]) < appRank(cols[j]) })

	writeJSONAPI(w, http.StatusOK, map[string]any{
		"benches": benchOut,
		"columns": cols,
		"matrix":  matrix,
		"backups": backups,
	})
}

func appVersionsList(names []string, ver map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]string{"name": n, "version": ver[n]})
	}
	return out
}

// appVersionsListFull extends appVersionsList with branch and latest_commit info.
func appVersionsListFull(names []string, ver, branch, latest map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]string{
			"name":          n,
			"version":       ver[n],
			"branch":        branch[n],
			"latest_commit": latest[n],
		})
	}
	return out
}

// benchEnvGuess maps a bench name to an environment label by common naming.
func benchEnvGuess(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.Contains(l, "prod"):
		return "production"
	case strings.Contains(l, "stag"):
		return "staging"
	case strings.Contains(l, "preview") || strings.Contains(l, "pr-") || strings.Contains(l, "dev"):
		return "preview"
	default:
		return ""
	}
}

func appRank(a string) int {
	switch a {
	case "frappe":
		return 0
	case "erpnext":
		return 1
	case "hrms":
		return 2
	default:
		return 10
	}
}

func nullStrVal(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func (s *Server) apiBenches(w http.ResponseWriter, r *http.Request) {
	benches, _ := s.db.ReadQ.ListBenches(r.Context())
	out := make([]any, 0, len(benches))
	for _, b := range benches {
		out = append(out, map[string]any{"id": b.ID, "name": b.Name, "path": b.Path, "server_id": b.ServerID})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

// --- deploys & jobs ---

func (s *Server) apiDeploys(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.db.ReadQ.ListRecentDeploys(r.Context(), 200)
	out := make([]any, 0, len(rows))
	for _, d := range rows {
		errStr := ""
		if d.LastError.Valid {
			errStr = d.LastError.String
		}
		out = append(out, map[string]any{
			"id":          d.ID,
			"kind":        d.Kind,
			"target_type": d.TargetType,
			"target_id":   d.TargetID,
			"trigger":     d.Trigger,
			"status":      d.Status,
			"error":       errStr,
			"created_at":  d.CreatedAt,
			"last_job_id": d.LastJobID,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiJobDetail(w http.ResponseWriter, r *http.Request) {
	job, err := s.db.ReadQ.GetJob(r.Context(), pathID(r))
	if err != nil {
		apiError(w, http.StatusNotFound, "job not found")
		return
	}
	logs, _ := s.db.ReadQ.ListJobLogs(r.Context(), store.ListJobLogsParams{JobID: job.ID, Seq: 0})
	lines := make([]any, 0, len(logs))
	for _, l := range logs {
		lines = append(lines, map[string]any{"seq": l.Seq, "stream": l.Stream, "line": l.Line, "ts": l.Ts})
	}
	errStr := ""
	if job.Error.Valid {
		errStr = job.Error.String
	}
	out := map[string]any{
		"id":       job.ID,
		"type":     job.Type,
		"status":   job.Status,
		"error":    errStr,
		"logs":     lines,
		"started":  job.StartedAt.Int64,
		"finished": job.FinishedAt.Int64,
	}
	// Enrich with deploy context (commit, trigger, target) for the header.
	if job.DeployID.Valid {
		if dep, err := s.db.ReadQ.GetDeploy(r.Context(), job.DeployID.Int64); err == nil {
			out["deploy_id"] = dep.ID
			out["kind"] = dep.Kind
			out["commit"] = dep.CommitSha.String
			out["trigger"] = dep.Trigger
			out["target_type"] = dep.TargetType
			out["target_id"] = dep.TargetID
			out["created_at"] = dep.CreatedAt
		}
	}
	writeJSONAPI(w, http.StatusOK, out)
}

// --- servers ---

func (s *Server) apiServers(w http.ResponseWriter, r *http.Request) {
	servers, _ := s.db.ReadQ.ListServers(r.Context())
	out := make([]any, 0, len(servers))
	for _, sv := range servers {
		ver := ""
		if sv.AgentVersion.Valid {
			ver = sv.AgentVersion.String
		}
		out = append(out, map[string]any{
			"id": sv.ID, "name": sv.Name, "hostname": sv.Hostname,
			"status": sv.Status, "agent_version": ver,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiServerRefresh(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := pathID(r)
	if _, err := s.RunServerStatus(r.Context(), id, user.ID); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "server.refresh", "server", id, "")
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) apiServerImport(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	b, si, err := s.ImportDiscovered(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "server.import", "server", id, "")
	writeJSONAPI(w, http.StatusOK, map[string]any{"benches": b, "sites": si})
}

func (s *Server) apiServerAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Hostname == "" {
		in.Hostname = in.Name
	}
	id, token, err := s.CreateEnrollment(r.Context(), in.Name, in.Hostname)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "server.add", "server", id, in.Name)
	cmd := "sudo -u frappe ops-agent register --panel " + s.cfg.BaseURL + " --token " + token
	writeJSONAPI(w, http.StatusOK, map[string]any{"id": id, "enroll_cmd": cmd})
}

// --- app sources, web apps, routes ---

func (s *Server) apiAppSources(w http.ResponseWriter, r *http.Request) {
	sources, _ := s.db.ReadQ.ListAppSources(r.Context())
	hasToken := s.ghToken(r.Context()) != ""
	out := make([]any, 0, len(sources))
	for _, src := range sources {
		row := map[string]any{
			"id": src.ID, "name": src.Name, "repo": src.Repo,
			"branch": src.Branch, "kind": src.Kind, "auto_deploy": src.AutoDeploy != 0,
		}
		// Best-effort: show the latest commit on the branch when GitHub is
		// connected and the repo looks like a GitHub slug.
		if hasToken && validRepo(src.Repo) {
			row["latest_commit"] = s.ghLatestCommit(r.Context(), src.Repo, src.Branch)
		}
		out = append(out, row)
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiAppSourceAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name       string `json:"name"`
		Repo       string `json:"repo"`
		Branch     string `json:"branch"`
		Kind       string `json:"kind"`
		AutoDeploy bool   `json:"auto_deploy"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	_, err := s.db.WriteQ.CreateAppSource(r.Context(), store.CreateAppSourceParams{
		Name: strings.TrimSpace(in.Name), Repo: strings.TrimSpace(in.Repo),
		Branch: orDefault(strings.TrimSpace(in.Branch), "main"), Kind: in.Kind,
		AutoDeploy: boolToInt(in.AutoDeploy),
	})
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "app_source.add", "app_source", 0, in.Repo)
	writeJSONAPI(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiWebApps(w http.ResponseWriter, r *http.Request) {
	apps, _ := s.db.ReadQ.ListWebApps(r.Context())
	out := make([]any, 0, len(apps))
	for _, a := range apps {
		commit := ""
		if a.CurrentCommit.Valid {
			commit = a.CurrentCommit.String
		}
		out = append(out, map[string]any{
			"id": a.ID, "name": a.Name, "internal_port": a.InternalPort,
			"status": a.Status, "commit": commit,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiRoutes(w http.ResponseWriter, r *http.Request) {
	routes, _ := s.db.ReadQ.ListProxyRoutes(r.Context())
	out := make([]any, 0, len(routes))
	for _, rt := range routes {
		out = append(out, map[string]any{
			"id": rt.ID, "domain": rt.Domain, "upstream": rt.Upstream,
			"ssl": rt.SslEnabled != 0, "status": rt.Status,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiRouteApply(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := pathID(r)
	var in struct {
		LeEmail string `json:"le_email"`
	}
	_ = decodeJSON(w, r, &in)
	if _, err := s.ApplyRoute(r.Context(), id, strings.TrimSpace(in.LeEmail), user.ID); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "route.apply", "route", id, "")
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"ok": true})
}

// --- users & audit ---

func (s *Server) apiUsers(w http.ResponseWriter, r *http.Request) {
	users, _ := s.db.ReadQ.ListUsers(r.Context())
	out := make([]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"id": u.ID, "email": u.Email, "role": u.Role, "disabled": u.Disabled != 0,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiAudit(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.db.ReadQ.ListAudit(r.Context(), store.ListAuditParams{Limit: 200, Offset: 0})
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"id": e.ID, "actor": e.Actor, "action": e.Action,
			"object_type": e.TargetType.String, "object_id": e.TargetID.Int64,
			"detail": e.DetailJson.String, "created_at": e.Ts,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

// --- pipelines ---

// envJSON builds the env payload with derived agent info and status. prevCommit
// is the commit of the previous (lower-rank) env, used to detect "behind".
func envJSON(e store.ListEnvsForPipelineRow, prevCommit string) map[string]any {
	cur := e.CurrentCommit.String
	// Derive a display status for the env card.
	status := "idle"
	switch {
	case e.DeployStatus.Valid && (e.DeployStatus.String == "running" || e.DeployStatus.String == "queued"):
		status = "deploying"
	case e.DeployStatus.Valid && e.DeployStatus.String == "failed":
		status = "failed"
	case prevCommit != "" && cur != prevCommit:
		status = "behind" // previous env has a newer commit awaiting promotion
	case cur != "":
		status = "healthy"
	}
	return map[string]any{
		"id":               e.ID,
		"name":             e.Name,
		"rank":             e.Rank,
		"site_id":          e.SiteID,
		"site_domain":      e.SiteDomain,
		"site_status":      e.SiteStatus,
		"require_approval": e.RequireApproval != 0,
		"current_commit":   cur,
		"last_deploy_id":   e.LastDeployID.Int64,
		"server_id":        e.ServerID,
		"server_name":      e.ServerName,
		"agent_online":     e.ServerStatus == "online",
		"status":           status,
	}
}

func (s *Server) pipelineListJSON(ctx context.Context, p store.Pipeline) map[string]any {
	envs, _ := s.db.ReadQ.ListEnvsForPipeline(ctx, p.ID)
	src, _ := s.db.ReadQ.GetAppSource(ctx, p.AppSourceID)
	envOut := make([]any, 0, len(envs))
	prev := ""
	for _, e := range envs {
		envOut = append(envOut, envJSON(e, prev))
		prev = e.CurrentCommit.String
	}
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"app":         src.Name,
		"app_source_id": p.AppSourceID,
		"created_at":  p.CreatedAt,
		"envs":        envOut,
	}
}

func (s *Server) apiPipelines(w http.ResponseWriter, r *http.Request) {
	pls, _ := s.db.ReadQ.ListPipelines(r.Context())
	out := make([]any, 0, len(pls))
	for _, p := range pls {
		out = append(out, s.pipelineListJSON(r.Context(), p))
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiPipelineDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.db.ReadQ.GetPipeline(ctx, pathID(r))
	if err != nil {
		apiError(w, http.StatusNotFound, "pipeline not found")
		return
	}
	out := s.pipelineListJSON(ctx, p)
	out["timeline"] = s.pipelineTimeline(ctx, p.ID)
	writeJSONAPI(w, http.StatusOK, out)
}

// pipelineTimeline builds the "change journey": for each commit promoted into
// this pipeline, which env nodes it reached, with the run, who, when and status.
// Derived entirely from pipeline_promote deploys targeting the pipeline's env
// sites — no extra schema.
func (s *Server) pipelineTimeline(ctx context.Context, pipelineID int64) []map[string]any {
	envs, _ := s.db.ReadQ.ListEnvsForPipeline(ctx, pipelineID)
	// map site_id -> env name/rank for this pipeline
	type envref struct{ name string; rank int64 }
	siteEnv := map[int64]envref{}
	order := make([]string, 0, len(envs))
	for _, e := range envs {
		siteEnv[e.SiteID] = envref{e.Name, e.Rank}
		order = append(order, e.Name)
	}

	deploys, _ := s.db.ReadQ.ListRecentDeploys(ctx, 500)
	// commit -> env name -> hop info
	type hop struct {
		Env    string `json:"env"`
		Rank   int64  `json:"rank"`
		Status string `json:"status"`
		Run    int64  `json:"run"`
		By     string `json:"by"`
		At     int64  `json:"at"`
	}
	byCommit := map[string]map[string]hop{}
	commitFirstSeen := map[string]int64{}
	for _, d := range deploys {
		if d.Kind != "pipeline_promote" || d.TargetType != "site" {
			continue
		}
		ref, ok := siteEnv[d.TargetID]
		if !ok {
			continue
		}
		commit := d.CommitSha.String
		if commit == "" {
			continue
		}
		who := ""
		if d.UserID.Valid {
			if u, err := s.db.ReadQ.GetUserByID(ctx, d.UserID.Int64); err == nil {
				who = u.Email
			}
		}
		if byCommit[commit] == nil {
			byCommit[commit] = map[string]hop{}
			commitFirstSeen[commit] = d.CreatedAt
		}
		// keep the most recent deploy per (commit, env)
		cur, exists := byCommit[commit][ref.name]
		if !exists || d.CreatedAt > cur.At {
			byCommit[commit][ref.name] = hop{ref.name, ref.rank, d.Status, d.LastJobID, who, d.CreatedAt}
		}
		if d.CreatedAt > commitFirstSeen[commit] {
			commitFirstSeen[commit] = d.CreatedAt
		}
	}

	out := make([]map[string]any, 0, len(byCommit))
	for commit, hops := range byCommit {
		row := make([]map[string]any, 0, len(order))
		for _, name := range order {
			if h, ok := hops[name]; ok {
				row = append(row, map[string]any{
					"env": h.Env, "status": h.Status, "run": h.Run, "by": h.By, "at": h.At, "reached": true,
				})
			} else {
				row = append(row, map[string]any{"env": name, "reached": false})
			}
		}
		out = append(out, map[string]any{
			"commit": commit, "updated_at": commitFirstSeen[commit], "hops": row,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["updated_at"].(int64) > out[j]["updated_at"].(int64)
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func (s *Server) apiPipelineCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		AppSourceID int64  `json:"app_source_id"`
		Envs        []struct {
			Name            string `json:"name"`
			SiteID          int64  `json:"site_id"`
			RequireApproval bool   `json:"require_approval"`
		} `json:"envs"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	specs := make([]EnvSpec, 0, len(in.Envs))
	for _, e := range in.Envs {
		specs = append(specs, EnvSpec{Name: e.Name, SiteID: e.SiteID, RequireApproval: e.RequireApproval})
	}
	id, err := s.CreatePipeline(r.Context(), strings.TrimSpace(in.Name), in.AppSourceID, specs)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "pipeline.create", "pipeline", id, in.Name)
	writeJSONAPI(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) apiPipelinePromote(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := pathID(r)
	var in struct {
		EnvRank int64  `json:"env_rank"`
		Commit  string `json:"commit"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	depID, err := s.PromoteEnv(r.Context(), id, in.EnvRank, strings.TrimSpace(in.Commit), user.ID)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "pipeline.promote", "pipeline", id, in.Commit)
	writeJSONAPI(w, http.StatusAccepted, map[string]any{"deploy_id": depID})
}

func (s *Server) apiPipelineDelete(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	_ = s.db.WriteQ.DeletePipelineEnvs(r.Context(), id)
	if err := s.db.WriteQ.DeletePipeline(r.Context(), id); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "pipeline.delete", "pipeline", id, "")
	writeJSONAPI(w, http.StatusOK, map[string]any{"ok": true})
}

// apiRoutes registers the JSON API under /api/v1. Called from routes().
func (s *Server) registerAPIRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/v1/me", s.requireRole(auth.RoleViewer, s.apiMe))
	m.HandleFunc("GET /api/v1/dashboard", s.requireRole(auth.RoleViewer, s.apiDashboard))

	m.HandleFunc("GET /api/v1/sites", s.requireRole(auth.RoleViewer, s.apiSites))
	m.HandleFunc("GET /api/v1/sites/{id}", s.requireRole(auth.RoleViewer, s.apiSiteDetail))
	m.HandleFunc("POST /api/v1/sites", s.requireRole(auth.RoleAdmin, s.apiNewSite))
	m.HandleFunc("POST /api/v1/sites/{id}/delete", s.requireRole(auth.RoleAdmin, s.apiSiteDelete))
	m.HandleFunc("POST /api/v1/sites/{id}/backup", s.requireRole(auth.RoleDeployer, s.apiSiteBackup))
	m.HandleFunc("POST /api/v1/sites/{id}/refresh", s.requireRole(auth.RoleAdmin, s.apiSiteRefresh))
	m.HandleFunc("POST /api/v1/sites/{id}/apps/{app}/rollback", s.requireRole(auth.RoleDeployer, s.apiSiteRollback))
	m.HandleFunc("GET /api/v1/benches", s.requireRole(auth.RoleViewer, s.apiBenches))
	m.HandleFunc("GET /api/v1/benches/overview", s.requireRole(auth.RoleViewer, s.apiBenchesOverview))

	m.HandleFunc("GET /api/v1/deploys", s.requireRole(auth.RoleViewer, s.apiDeploys))
	m.HandleFunc("GET /api/v1/jobs/{id}", s.requireRole(auth.RoleViewer, s.apiJobDetail))

	m.HandleFunc("GET /api/v1/servers", s.requireRole(auth.RoleViewer, s.apiServers))
	m.HandleFunc("GET /api/v1/servers/fleet", s.requireRole(auth.RoleViewer, s.apiFleet))
	m.HandleFunc("POST /api/v1/servers/install-command", s.requireRole(auth.RoleAdmin, s.apiInstallCommand))
	m.HandleFunc("POST /api/v1/servers", s.requireRole(auth.RoleAdmin, s.apiServerAdd))
	m.HandleFunc("POST /api/v1/servers/{id}/refresh", s.requireRole(auth.RoleAdmin, s.apiServerRefresh))
	m.HandleFunc("POST /api/v1/servers/{id}/import", s.requireRole(auth.RoleAdmin, s.apiServerImport))

	m.HandleFunc("GET /api/v1/app-sources", s.requireRole(auth.RoleViewer, s.apiAppSources))
	m.HandleFunc("POST /api/v1/app-sources", s.requireRole(auth.RoleAdmin, s.apiAppSourceAdd))
	m.HandleFunc("GET /api/v1/web-apps", s.requireRole(auth.RoleViewer, s.apiWebApps))
	m.HandleFunc("GET /api/v1/routes", s.requireRole(auth.RoleViewer, s.apiRoutes))
	m.HandleFunc("POST /api/v1/routes/{id}/apply", s.requireRole(auth.RoleAdmin, s.apiRouteApply))

	m.HandleFunc("GET /api/v1/users", s.requireRole(auth.RoleAdmin, s.apiUsers))
	m.HandleFunc("GET /api/v1/audit", s.requireRole(auth.RoleViewer, s.apiAudit))

	m.HandleFunc("GET /api/v1/github/status", s.requireRole(auth.RoleViewer, s.apiGithubStatus))
	m.HandleFunc("POST /api/v1/github/token", s.requireRole(auth.RoleAdmin, s.apiGithubSetToken))
	m.HandleFunc("DELETE /api/v1/github/token", s.requireRole(auth.RoleAdmin, s.apiGithubDisconnect))
	m.HandleFunc("GET /api/v1/github/repos", s.requireRole(auth.RoleAdmin, s.apiGithubRepos))
	m.HandleFunc("GET /api/v1/github/branches", s.requireRole(auth.RoleAdmin, s.apiGithubBranches))
	m.HandleFunc("GET /api/v1/github/commits", s.requireRole(auth.RoleDeployer, s.apiGithubCommits))

	m.HandleFunc("GET /api/v1/pipelines", s.requireRole(auth.RoleViewer, s.apiPipelines))
	m.HandleFunc("GET /api/v1/pipelines/{id}", s.requireRole(auth.RoleViewer, s.apiPipelineDetail))
	m.HandleFunc("POST /api/v1/pipelines", s.requireRole(auth.RoleAdmin, s.apiPipelineCreate))
	m.HandleFunc("POST /api/v1/pipelines/{id}/promote", s.requireRole(auth.RoleDeployer, s.apiPipelinePromote))
	m.HandleFunc("DELETE /api/v1/pipelines/{id}", s.requireRole(auth.RoleAdmin, s.apiPipelineDelete))
}
