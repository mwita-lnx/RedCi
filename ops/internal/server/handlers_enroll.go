package server

import (
	"net/http"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/web/components"
)

// sessEnrollSecret holds the not-yet-confirmed TOTP secret during enrollment.
const sessEnrollSecret = "enroll_totp_secret"

// pendingEnrollSecret returns the enrollment secret for this session, creating
// and storing a fresh one if none exists yet.
func (s *Server) pendingEnrollSecret(r *http.Request, email string) (string, error) {
	if sec := s.sessions.GetString(r.Context(), sessEnrollSecret); sec != "" {
		return sec, nil
	}
	key, err := auth.GenerateTOTP(email)
	if err != nil {
		return "", err
	}
	s.sessions.Put(r.Context(), sessEnrollSecret, key.Secret())
	return key.Secret(), nil
}

func (s *Server) handleEnrollForm(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	if s.auth.HasTOTP(user) { // already enrolled
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	secret, err := s.pendingEnrollSecret(r, user.Email)
	if err != nil {
		http.Error(w, "could not start enrollment", http.StatusInternalServerError)
		return
	}
	render(w, r, components.EnrollTOTP(csrfField(r), secret, ""))
}

// handleEnrollQR renders the QR PNG for the pending enrollment secret.
func (s *Server) handleEnrollQR(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	secret := s.sessions.GetString(r.Context(), sessEnrollSecret)
	if secret == "" {
		http.NotFound(w, r)
		return
	}
	key, err := auth.TOTPFromSecret(user.Email, secret)
	if err != nil {
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}
	png, err := auth.QRCodePNG(key, 200)
	if err != nil {
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) handleEnrollSubmit(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	secret := s.sessions.GetString(r.Context(), sessEnrollSecret)
	if secret == "" {
		http.Redirect(w, r, "/enroll-totp", http.StatusSeeOther)
		return
	}
	if err := s.auth.EnrollTOTP(r.Context(), user.ID, secret, r.PostFormValue("code")); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, components.EnrollTOTP(csrfField(r), secret, "That code didn't match. Scan the QR again and retry."))
		return
	}
	s.sessions.Remove(r.Context(), sessEnrollSecret)
	s.audit(r, "user.totp_enrolled", "user", user.ID, "")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
