package server

import (
	"net/http"

	"filippo.io/csrf"

	"github.com/mwita-lnx/RedCi/ops/web/static"
)

// routes registers every handler on the mux. CSRF protection wraps the whole
// mux (it only acts on non-GET requests), so all form posts are protected.
func (s *Server) routes() {
	// Static CSS for the server-rendered auth pages (login / TOTP / enrollment).
	// The rest of the UI is the React SPA at /app/ with its own bundled assets.
	s.mux.Handle("GET /static/", http.StripPrefix("/static/",
		http.FileServer(http.FS(static.FS))))

	// Health check for an external uptime monitor — no auth.
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Agent install bootstrap — public (a new server has no session; the
	// one-time token in the piped command is the credential).
	s.mux.HandleFunc("GET /agent.sh", s.handleAgentScript)
	s.mux.HandleFunc("GET /agent/download/{arch}", s.handleAgentDownload)

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

	// The old server-rendered (templ) UI has been removed in favour of the React
	// SPA at /app/. Anything landing at the root is sent there; the SPA handles
	// all pages client-side and talks to the JSON API below.
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusSeeOther)
	})

	// Agent API (VPN-only in production; bearer-token auth, no session/CSRF).
	s.mux.HandleFunc("POST /agent/v1/register", s.handleAgentRegister)
	s.mux.HandleFunc("POST /agent/v1/heartbeat", s.requireAgent(s.handleAgentHeartbeat))
	s.mux.HandleFunc("POST /agent/v1/discovery", s.requireAgent(s.handleAgentDiscovery))
	s.mux.HandleFunc("GET /agent/v1/jobs/next", s.requireAgent(s.handleAgentJobsNext))
	s.mux.HandleFunc("POST /agent/v1/jobs/{id}/start", s.requireAgent(s.handleAgentJobStart))
	s.mux.HandleFunc("POST /agent/v1/jobs/{id}/logs", s.requireAgent(s.handleAgentJobLogs))
	s.mux.HandleFunc("POST /agent/v1/jobs/{id}/result", s.requireAgent(s.handleAgentJobResult))

	// JSON API for the React SPA (session-auth, same-origin CSRF).
	s.registerAPIRoutes()

	// React SPA (served under /app/, client-side routed). Requires login; the
	// SPA's own API calls enforce per-action roles.
	s.mux.HandleFunc("GET /app/", s.requireLogin(s.handleSPA))
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
