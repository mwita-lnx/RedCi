package server

import (
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
	dbPw := r.PostFormValue("db_root_password")
	leEmail := strings.TrimSpace(r.PostFormValue("le_email"))
	ssl := r.PostFormValue("ssl") == "on"

	depID, err := s.CreateSiteDeploy(r.Context(), benchID, domain, apps, adminPw, dbPw, leEmail, ssl, user.ID)
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
