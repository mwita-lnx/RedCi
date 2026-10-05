package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mwita-lnx/RedCi/ops/internal/auth"
	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/ops/web/components"
)

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	users, _ := s.db.ReadQ.ListUsers(r.Context())
	render(w, r, components.Users(user, users, ""))
}

func (s *Server) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	email := strings.TrimSpace(r.PostFormValue("email"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	password := r.PostFormValue("password")
	role := auth.Role(r.PostFormValue("role"))

	if len(password) < 8 {
		s.renderUsers(w, r, user, "Password must be at least 8 characters.")
		return
	}
	if _, err := s.auth.CreateUser(r.Context(), email, name, password, role); err != nil {
		s.renderUsers(w, r, user, err.Error())
		return
	}
	s.audit(r, "user.create", "user", 0, email)
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (s *Server) handleUserResetTOTP(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.db.WriteQ.ClearUserTOTP(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "user.reset_totp", "user", id, "")
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (s *Server) handleUserToggleDisabled(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	target, err := s.db.ReadQ.GetUserByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Don't let an admin disable their own account.
	if cur, ok := userFrom(r.Context()); ok && cur.ID == id {
		http.Error(w, "cannot disable your own account", http.StatusBadRequest)
		return
	}
	newVal := int64(1)
	if target.Disabled != 0 {
		newVal = 0
	}
	if err := s.db.WriteQ.SetUserDisabled(r.Context(), store.SetUserDisabledParams{Disabled: newVal, ID: id}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r, "user.set_disabled", "user", id, strconv.FormatInt(newVal, 10))
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (s *Server) renderUsers(w http.ResponseWriter, r *http.Request, user store.User, errMsg string) {
	users, _ := s.db.ReadQ.ListUsers(r.Context())
	w.WriteHeader(http.StatusBadRequest)
	render(w, r, components.Users(user, users, errMsg))
}
