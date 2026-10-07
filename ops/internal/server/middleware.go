package server

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
)

// requestID attaches a short random ID to each request for correlation.
func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(withRequestID(r.Context(), id)))
	})
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// Flush exposes http.Flusher for SSE streams added later.
func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		s.log.Info("request",
			"id", requestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", sr.status,
			"dur_ms", time.Since(start).Milliseconds(),
			"ip", clientIP(r),
		)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic recovered",
					"id", requestIDFrom(r.Context()),
					"err", rec,
					"path", r.URL.Path,
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets the strict CSP and related headers from the spec. The
// self-only CSP is possible because htmx and CSS are served from the binary.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if !s.cfg.Dev {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// requireLogin ensures a logged-in, TOTP-verified user, and enforces TOTP
// enrollment: a user without 2FA is redirected to the enrollment page and
// cannot reach any other protected page until they finish.
func (s *Server) requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.currentUser(r.Context())
		if !ok {
			// API clients get a clean 401 to redirect themselves; page
			// requests get a browser redirect to the login screen.
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// 2FA enrollment enforcement paused.
		next(w, r.WithContext(withUser(r.Context(), user)))
	}
}

// requireRole wraps requireLogin and additionally checks the user's role.
func (s *Server) requireRole(required auth.Role, next http.HandlerFunc) http.HandlerFunc {
	return s.requireLogin(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFrom(r.Context())
		if !auth.Role(user.Role).AtLeast(required) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// audit writes an audit_log row for a mutating action by the current user.
func (s *Server) audit(r *http.Request, action, targetType string, targetID int64, detail string) {
	params := store.InsertAuditParams{
		Actor:  "user",
		Action: action,
		Ip:     nullStr(clientIP(r)),
	}
	if u, ok := userFrom(r.Context()); ok {
		params.UserID = nullInt(u.ID)
	}
	if targetType != "" {
		params.TargetType = nullStr(targetType)
		params.TargetID = nullInt(targetID)
	}
	if detail != "" {
		params.DetailJson = nullStr(detail)
	}
	if err := s.db.WriteQ.InsertAudit(r.Context(), params); err != nil {
		s.log.Error("audit write failed", "action", action, "err", err)
	}
}

// clientIP extracts a best-effort client IP (honours X-Forwarded-For from the
// trusted front proxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := indexByte(xff, ','); i >= 0 {
			return trimSpace(xff[:i])
		}
		return trimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
