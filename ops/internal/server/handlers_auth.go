package server

import (
	"errors"
	"net/http"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/web/components"
)

// csrfField renders nothing: CSRF is enforced by origin checks
// (filippo.io/csrf), not a per-form token. The templ forms keep the slot so a
// token-based scheme could be dropped back in without touching the templates.
func csrfField(r *http.Request) templComponent {
	return rawHTML("")
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	// Already fully logged in? Go home.
	if _, ok := s.currentUser(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	render(w, r, components.Login(csrfField(r), ""))
}

func (s *Server) handleLoginPassword(w http.ResponseWriter, r *http.Request) {
	email := r.PostFormValue("email")
	password := r.PostFormValue("password")

	user, err := s.auth.Authenticate(r.Context(), email, password, clientIP(r))
	if err != nil {
		msg := "Invalid email or password."
		switch {
		case errors.Is(err, auth.ErrLockedOut):
			msg = "Too many attempts. Try again in a few minutes."
		case errors.Is(err, auth.ErrUserDisabled):
			msg = "This account is disabled."
		}
		s.audit(r, "user.login_failed", "", 0, email)
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, components.Login(csrfField(r), msg))
		return
	}

	// 2FA paused — log in directly regardless of TOTP enrollment status.
	s.completeLogin(w, r, user)
}

func (s *Server) handleTOTPForm(w http.ResponseWriter, r *http.Request) {
	if s.sessions.GetInt64(r.Context(), sessPendingTOTP) == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	render(w, r, components.TOTPVerify(csrfField(r), ""))
}

func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	uid := s.sessions.GetInt64(r.Context(), sessPendingTOTP)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	user, err := s.db.ReadQ.GetUserByID(r.Context(), uid)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	ok, err := s.auth.VerifyTOTPFor(r.Context(), user, r.PostFormValue("code"))
	if err != nil || !ok {
		s.audit(r, "user.totp_failed", "user", user.ID, "")
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, components.TOTPVerify(csrfField(r), "Incorrect code. Try again."))
		return
	}
	s.sessions.Remove(r.Context(), sessPendingTOTP)
	s.completeLogin(w, r, user)
}

// completeLogin rotates the session token, records the user, and audits.
func (s *Server) completeLogin(w http.ResponseWriter, r *http.Request, user storeUser) {
	_ = s.sessions.RenewToken(r.Context()) // rotate token on privilege change
	s.sessions.Put(r.Context(), sessUserID, user.ID)
	_ = s.auth.TouchLogin(r.Context(), user.ID)
	// Audit with the user already in context.
	ctx := withUser(r.Context(), user)
	s.audit(r.WithContext(ctx), "user.login", "user", user.ID, "")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.currentUser(r.Context()); ok {
		s.audit(r.WithContext(withUser(r.Context(), u)), "user.logout", "user", u.ID, "")
	}
	_ = s.sessions.Destroy(r.Context())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
