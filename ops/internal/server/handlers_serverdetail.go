package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/web/components"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// handleServerDetail shows a server's facts, benches, sites and routes.
func (s *Server) handleServerDetail(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	sv, err := s.db.ReadQ.GetServerByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	benches, _ := s.db.ReadQ.ListBenchesForServer(r.Context(), id)
	routes, _ := s.db.ReadQ.ListProxyRoutesForServer(r.Context(), id)

	d := components.ServerDetailData{Server: sv, Benches: benches, Routes: routes}
	// Sites per bench.
	for _, b := range benches {
		sites, _ := s.db.ReadQ.SitesForBench(r.Context(), b.ID)
		d.Sites = append(d.Sites, sites...)
	}
	render(w, r, components.ServerDetail(user, d))
}

// handleNginxFiles shows the read-only nginx config browser for a server,
// from the latest list_nginx_configs result, with each file tagged.
func (s *Server) handleNginxFiles(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	sv, err := s.db.ReadQ.GetServerByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var files []components.NginxFile
	row, err := s.db.ReadQ.LatestResultForType(r.Context(), store.LatestResultForTypeParams{
		ServerID: id, Type: string(jobs.TypeListNginxConfigs),
	})
	if err == nil && row.Valid {
		var parsed struct {
			Configs []struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				SHA256  string `json:"sha256"`
			} `json:"configs"`
		}
		if json.Unmarshal([]byte(row.String), &parsed) == nil {
			for _, c := range parsed.Configs {
				files = append(files, components.NginxFile{
					Path: c.Path, Content: c.Content, SHA256: c.SHA256, Tag: tagForPath(c.Path),
				})
			}
		}
	}
	render(w, r, components.NginxFiles(user, sv, files))
}

// handleNginxRefresh enqueues a list_nginx_configs job for a server.
func (s *Server) handleNginxRefresh(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := s.createDeploy(r.Context(), "list_nginx_configs", "server", id, "ui", "", user.ID, []jobSeed{
		{serverID: id, typ: jobs.TypeListNginxConfigs, params: jobs.ListNginxConfigsParams{}},
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.nudge()
	s.audit(r, "nginx.refresh", "server", id, "")
	http.Redirect(w, r, "/servers/"+strconv.FormatInt(id, 10)+"/nginx", http.StatusSeeOther)
}

// tagForPath classifies an nginx config path for the UI.
func tagForPath(path string) string {
	switch {
	case strings.Contains(path, "/ops.d/"):
		return "ops"
	case strings.Contains(path, "frappe-bench") || strings.Contains(path, "/conf.d/frappe"):
		return "bench"
	default:
		return "other"
	}
}
