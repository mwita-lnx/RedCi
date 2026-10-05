package server

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"

	ops "github.com/mwita-lnx/RedCi/ops/internal"
	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/events"
	"github.com/mwita-lnx/RedCi/ops/internal/secrets"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// Session keys used by scs.
const (
	sessUserID       = "user_id"
	sessPendingTOTP  = "pending_totp_user_id" // password ok, TOTP not yet verified this session
	sessReauthUntil  = "reauth_until"         // unix seconds; sensitive-action window
)

// Server wires the HTTP layer: router, session manager, auth service, store.
type Server struct {
	cfg      ops.Config
	db       *store.DB
	auth     *auth.Service
	sealer   *secrets.Sealer
	sessions *scs.SessionManager
	bus      *events.Bus
	log      *slog.Logger
	mux      *http.ServeMux
	nudger   Nudger

	// notify holds one buffered channel per server id, signalled by the worker
	// when it promotes a job so a waiting long-poll returns immediately.
	notifyMu sync.Mutex
	notify   map[int64]chan struct{}
}

// Nudger lets the server ask the worker to run its passes immediately (e.g.
// after a webhook or UI action creates jobs). The worker implements it.
type Nudger interface{ Nudge() }

// SetNudger wires the worker's Nudge into the server after both are built.
func (s *Server) SetNudger(n Nudger) { s.nudger = n }

// nudge kicks the worker if one is wired.
func (s *Server) nudge() {
	if s.nudger != nil {
		s.nudger.Nudge()
	}
}

// New builds a Server and its session manager. The session store lives in the
// same SQLite file (the `sessions` table from the first migration). bus and
// sealer are shared with the worker.
func New(cfg ops.Config, db *store.DB, authSvc *auth.Service, sealer *secrets.Sealer, bus *events.Bus, log *slog.Logger) *Server {
	sm := scs.New()
	sm.Store = sqlite3store.New(db.Writer)
	sm.Lifetime = 12 * time.Hour  // absolute lifetime
	sm.IdleTimeout = 1 * time.Hour // idle timeout
	sm.Cookie.Name = "ops_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = !cfg.Dev // always Secure except plain-HTTP local dev
	sm.Cookie.Path = "/"

	s := &Server{
		cfg:      cfg,
		db:       db,
		auth:     authSvc,
		sealer:   sealer,
		sessions: sm,
		bus:      bus,
		log:      log,
		mux:      http.NewServeMux(),
		notify:   make(map[int64]chan struct{}),
	}
	s.routes()
	return s
}

// notifyChan returns the notify channel for a server, creating it on first use.
func (s *Server) notifyChan(serverID int64) chan struct{} {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	ch := s.notify[serverID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.notify[serverID] = ch
	}
	return ch
}

// Wake signals a server's long-poll waiter that new work is ready. Called by
// the worker after it promotes a job.
func (s *Server) Wake(serverID int64) {
	select {
	case s.notifyChan(serverID) <- struct{}{}:
	default:
	}
}

// Handler returns the fully wrapped HTTP handler (global middleware + session).
func (s *Server) Handler() http.Handler {
	// Order (outermost first): request ID, logging, recovery, security headers,
	// then session load/save, then per-route middleware applied in routes().
	var h http.Handler = s.mux
	h = csrfProtect(s.cfg.BaseURL, h)
	h = s.sessions.LoadAndSave(h)
	h = s.securityHeaders(h)
	h = s.recoverPanic(h)
	h = s.logRequests(h)
	h = s.requestID(h)
	return h
}

// currentUser loads the logged-in, TOTP-verified user from the session.
func (s *Server) currentUser(ctx context.Context) (store.User, bool) {
	id := s.sessions.GetInt64(ctx, sessUserID)
	if id == 0 {
		return store.User{}, false
	}
	u, err := s.db.ReadQ.GetUserByID(ctx, id)
	if err != nil || u.Disabled != 0 {
		return store.User{}, false
	}
	return u, true
}
