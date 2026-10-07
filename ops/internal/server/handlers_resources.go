package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/web/components"
)

// --- Servers ---

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	servers, _ := s.db.ReadQ.ListServers(r.Context())
	render(w, r, components.Servers(user, servers))
}

func (s *Server) handleServerRefresh(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := s.RunServerStatus(r.Context(), id, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "server.refresh", "server", id, "")
	http.Redirect(w, r, "/servers", http.StatusSeeOther)
}

func (s *Server) handleServerImport(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	b, si, err := s.ImportDiscovered(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "server.import", "server", id, "")
	s.log.Info("imported", "server", id, "benches", b, "sites", si)
	http.Redirect(w, r, "/servers", http.StatusSeeOther)
}

func (s *Server) handleServerAddForm(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	render(w, r, components.ServerAdd(user, "", ""))
}

func (s *Server) handleServerAddSubmit(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	name := strings.TrimSpace(r.PostFormValue("name"))
	hostname := strings.TrimSpace(r.PostFormValue("hostname"))
	if hostname == "" {
		hostname = name
	}
	id, token, err := s.CreateEnrollment(r.Context(), name, hostname)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, components.ServerAdd(user, "", err.Error()))
		return
	}
	s.audit(r, "server.add", "server", id, name)
	cmd := "sudo -u frappe ops-agent register --panel " + s.cfg.BaseURL + " --token " + token
	render(w, r, components.ServerAdd(user, cmd, ""))
}

// --- Sites ---

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	sites, _ := s.db.ReadQ.ListSites(r.Context())
	render(w, r, components.Sites(user, sites))
}

func (s *Server) handleNewSiteForm(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	benches, _ := s.db.ReadQ.ListBenches(r.Context())
	render(w, r, components.NewSite(user, benches, ""))
}

func (s *Server) handleNewSiteSubmit(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	benchID, _ := strconv.ParseInt(r.PostFormValue("bench_id"), 10, 64)
	domain := strings.TrimSpace(r.PostFormValue("domain"))
	apps := splitApps(r.PostFormValue("apps"))
	adminPw := r.PostFormValue("admin_password")
	dbRootUser := strings.TrimSpace(r.PostFormValue("db_root_user"))
	dbPw := r.PostFormValue("db_root_password")
	leEmail := strings.TrimSpace(r.PostFormValue("le_email"))
	ssl := r.PostFormValue("ssl") == "on"
	if dbRootUser == "" {
		dbRootUser = "root"
	}

	depID, err := s.CreateSiteDeploy(r.Context(), benchID, domain, apps, adminPw, dbRootUser, dbPw, leEmail, ssl, user.ID)
	if err != nil {
		benches, _ := s.db.ReadQ.ListBenches(r.Context())
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, components.NewSite(user, benches, err.Error()))
		return
	}
	s.audit(r, "site.create", "site", 0, domain)
	http.Redirect(w, r, "/deploys", http.StatusSeeOther)
	_ = depID
}

func (s *Server) handleSiteRefresh(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	site, err := s.db.ReadQ.GetSite(r.Context(), pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	bench, err := s.db.ReadQ.GetBench(r.Context(), site.BenchID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.RunServerStatus(r.Context(), bench.ServerID, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "site.refresh", "site", site.ID, site.Domain)
	http.Redirect(w, r, "/deploys", http.StatusSeeOther)
}

func (s *Server) handleSiteAppRollback(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	site, err := s.db.ReadQ.GetSite(r.Context(), pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	app := r.PathValue("app")
	if _, err := s.RollbackFrappeApp(r.Context(), site.BenchID, app, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "app.rollback", "bench", site.BenchID, app)
	http.Redirect(w, r, "/deploys", http.StatusSeeOther)
}

func (s *Server) handleSiteDetail(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	site, err := s.db.ReadQ.GetSite(r.Context(), pathID(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var installedApps []string
	_ = json.Unmarshal([]byte(site.AppsJson), &installedApps)

	// Build version map from frappe_apps rows (current_commit holds the version
	// string populated during server_status import).
	frappeApps, _ := s.db.ReadQ.ListFrappeAppsForBench(r.Context(), site.BenchID)
	appVerMap := make(map[string]string, len(frappeApps))
	for _, fa := range frappeApps {
		if fa.CurrentCommit.Valid {
			appVerMap[fa.AppName] = fa.CurrentCommit.String
		} else {
			appVerMap[fa.AppName] = ""
		}
	}
	// Build versions map filtered to this site's installed apps.
	versions := make(map[string]string, len(installedApps))
	for _, app := range installedApps {
		versions[app] = appVerMap[app]
	}
	// Apps with a recorded rollback point can be rolled back.
	rollbackable := make(map[string]bool, len(installedApps))
	for _, app := range installedApps {
		if _, err := s.db.ReadQ.GetDeployPoint(r.Context(), store.GetDeployPointParams{
			BenchID: site.BenchID, AppName: app,
		}); err == nil {
			rollbackable[app] = true
		}
	}
	render(w, r, components.SiteDetail(user, site, versions, rollbackable))
}

func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := pathID(r)
	dbRootUser := strings.TrimSpace(r.PostFormValue("db_root_user"))
	dbRootPassword := r.PostFormValue("db_root_password")
	if dbRootUser == "" {
		dbRootUser = "root"
	}
	if _, err := s.DeleteSite(r.Context(), id, dbRootUser, dbRootPassword, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "site.delete", "site", id, "")
	http.Redirect(w, r, "/sites", http.StatusSeeOther)
}

func (s *Server) handleSiteBackup(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	withFiles := r.PostFormValue("with_files") == "on"
	if _, err := s.BackupSite(r.Context(), id, withFiles, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "site.backup", "site", id, "")
	http.Redirect(w, r, "/deploys", http.StatusSeeOther)
}

// --- App sources ---

func (s *Server) handleAppSources(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	sources, _ := s.db.ReadQ.ListAppSources(r.Context())
	render(w, r, components.AppSources(user, sources))
}

func (s *Server) handleAppSourceAdd(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	_, err := s.db.WriteQ.CreateAppSource(r.Context(), store.CreateAppSourceParams{
		Name:       strings.TrimSpace(r.PostFormValue("name")),
		Repo:       strings.TrimSpace(r.PostFormValue("repo")),
		Branch:     orDefault(strings.TrimSpace(r.PostFormValue("branch")), "main"),
		Kind:       r.PostFormValue("kind"),
		AutoDeploy: boolToInt(r.PostFormValue("auto_deploy") == "on"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "app_source.add", "app_source", 0, r.PostFormValue("repo"))
	http.Redirect(w, r, "/app-sources", http.StatusSeeOther)
	_ = user
}

// --- Web apps ---

func (s *Server) handleWebApps(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	apps, _ := s.db.ReadQ.ListWebApps(r.Context())
	render(w, r, components.WebApps(user, apps))
}

// --- Routes ---

func (s *Server) handleRoutes(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	routes, _ := s.db.ReadQ.ListProxyRoutes(r.Context())
	render(w, r, components.Routes(user, routes))
}

func (s *Server) handleRouteApply(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	leEmail := strings.TrimSpace(r.PostFormValue("le_email"))
	if _, err := s.ApplyRoute(r.Context(), id, leEmail, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "route.apply", "route", id, "")
	http.Redirect(w, r, "/deploys", http.StatusSeeOther)
}

// helpers
func splitApps(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ensure auth import is used (role constants referenced in routes).
var _ = auth.RoleAdmin
