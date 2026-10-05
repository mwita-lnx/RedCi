package server

import (
	"net/http"

	"filippo.io/csrf"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/web/static"
)

// routes registers every handler on the mux. CSRF protection wraps the whole
// mux (it only acts on non-GET requests), so all form posts are protected.
func (s *Server) routes() {
	// Static assets (htmx, tailwind) embedded in the binary.
	s.mux.Handle("GET /static/", http.StripPrefix("/static/",
		http.FileServer(http.FS(static.FS))))

	// Health check for an external uptime monitor — no auth.
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	// GitHub push webhook — the only public endpoint; HMAC-authenticated.
	s.mux.HandleFunc("POST /webhooks/github", s.handleGithubWebhook)

	// Auth flow (public).
	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLoginPassword)
	s.mux.HandleFunc("GET /login/totp", s.handleTOTPForm)
	s.mux.HandleFunc("POST /login/totp", s.handleTOTPVerify)
	s.mux.HandleFunc("POST /logout", s.handleLogout)

	// First-login TOTP enrollment (logged in, but not yet enrolled).
	s.mux.HandleFunc("GET /enroll-totp", s.requireLogin(s.handleEnrollForm))
	s.mux.HandleFunc("POST /enroll-totp", s.requireLogin(s.handleEnrollSubmit))
	s.mux.HandleFunc("GET /enroll-totp/qr", s.requireLogin(s.handleEnrollQR))

	// Protected pages.
	s.mux.HandleFunc("GET /{$}", s.requireRole(auth.RoleViewer, s.handleDashboard))
	s.mux.HandleFunc("GET /audit", s.requireRole(auth.RoleViewer, s.handleAuditLog))
	s.mux.HandleFunc("GET /deploys", s.requireRole(auth.RoleViewer, s.handleDeploys))
	s.mux.HandleFunc("GET /jobs/{id}", s.requireRole(auth.RoleViewer, s.handleJobDetail))
	s.mux.HandleFunc("GET /jobs/{id}/stream", s.requireRole(auth.RoleViewer, s.handleJobStream))

	// Servers (admin to add/import; viewer to see).
	s.mux.HandleFunc("GET /servers", s.requireRole(auth.RoleViewer, s.handleServers))
	s.mux.HandleFunc("GET /servers/add", s.requireRole(auth.RoleAdmin, s.handleServerAddForm))
	s.mux.HandleFunc("POST /servers/add", s.requireRole(auth.RoleAdmin, s.handleServerAddSubmit))
	s.mux.HandleFunc("POST /servers/{id}/refresh", s.requireRole(auth.RoleAdmin, s.handleServerRefresh))
	s.mux.HandleFunc("POST /servers/{id}/import", s.requireRole(auth.RoleAdmin, s.handleServerImport))
	s.mux.HandleFunc("GET /servers/{id}", s.requireRole(auth.RoleViewer, s.handleServerDetail))
	s.mux.HandleFunc("GET /servers/{id}/nginx", s.requireRole(auth.RoleViewer, s.handleNginxFiles))
	s.mux.HandleFunc("POST /servers/{id}/nginx/refresh", s.requireRole(auth.RoleAdmin, s.handleNginxRefresh))

	// Sites.
	s.mux.HandleFunc("GET /sites", s.requireRole(auth.RoleViewer, s.handleSites))
	s.mux.HandleFunc("GET /sites/new", s.requireRole(auth.RoleAdmin, s.handleNewSiteForm))
	s.mux.HandleFunc("POST /sites/new", s.requireRole(auth.RoleAdmin, s.handleNewSiteSubmit))
	s.mux.HandleFunc("POST /sites/{id}/backup", s.requireRole(auth.RoleDeployer, s.handleSiteBackup))

	// App sources, web apps, routes.
	s.mux.HandleFunc("GET /app-sources", s.requireRole(auth.RoleViewer, s.handleAppSources))
	s.mux.HandleFunc("POST /app-sources/add", s.requireRole(auth.RoleAdmin, s.handleAppSourceAdd))
	s.mux.HandleFunc("GET /web-apps", s.requireRole(auth.RoleViewer, s.handleWebApps))
	s.mux.HandleFunc("GET /routes", s.requireRole(auth.RoleViewer, s.handleRoutes))
	s.mux.HandleFunc("POST /routes/{id}/apply", s.requireRole(auth.RoleAdmin, s.handleRouteApply))

	// Users and settings (admin only).
	s.mux.HandleFunc("GET /users", s.requireRole(auth.RoleAdmin, s.handleUsers))
	s.mux.HandleFunc("POST /users/add", s.requireRole(auth.RoleAdmin, s.handleUserAdd))
	s.mux.HandleFunc("POST /users/{id}/reset-totp", s.requireRole(auth.RoleAdmin, s.handleUserResetTOTP))
	s.mux.HandleFunc("POST /users/{id}/toggle", s.requireRole(auth.RoleAdmin, s.handleUserToggleDisabled))

	// Agent API (VPN-only in production; bearer-token auth, no session/CSRF).
	s.mux.HandleFunc("POST /agent/v1/register", s.handleAgentRegister)
	s.mux.HandleFunc("POST /agent/v1/heartbeat", s.requireAgent(s.handleAgentHeartbeat))
	s.mux.HandleFunc("GET /agent/v1/jobs/next", s.requireAgent(s.handleAgentJobsNext))
	s.mux.HandleFunc("POST /agent/v1/jobs/{id}/start", s.requireAgent(s.handleAgentJobStart))
	s.mux.HandleFunc("POST /agent/v1/jobs/{id}/logs", s.requireAgent(s.handleAgentJobLogs))
	s.mux.HandleFunc("POST /agent/v1/jobs/{id}/result", s.requireAgent(s.handleAgentJobResult))
}

// csrfProtect wraps the handler with filippo.io/csrf, which rejects non-safe
// cross-origin browser requests using the Sec-Fetch-Site / Origin headers
// (equivalent to Go 1.25's http.CrossOriginProtection). No per-form token is
// needed: safe methods and same-origin posts pass; cross-origin posts fail.
func csrfProtect(baseURL string, h http.Handler) http.Handler {
	p := csrf.New()
	if baseURL != "" {
		_ = p.AddTrustedOrigin(baseURL)
	}
	return p.Handler(h)
}
