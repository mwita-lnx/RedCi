package server

import (
	"net/http"
	"strings"
)

// GitHub integration API: save/check the PAT and browse repos/branches/commits
// so the SPA can pick an app, a branch, and a commit without free-text entry.

func (s *Server) apiGithubStatus(w http.ResponseWriter, r *http.Request) {
	tok := s.ghToken(r.Context())
	resp := map[string]any{"connected": tok != ""}
	if tok != "" {
		// Confirm the token actually works and report the login.
		var u struct {
			Login string `json:"login"`
		}
		if err := s.ghGet(r.Context(), "/user", &u); err != nil {
			resp["connected"] = false
			resp["error"] = err.Error()
		} else {
			resp["login"] = u.Login
		}
	}
	writeJSONAPI(w, http.StatusOK, resp)
}

func (s *Server) apiGithubSetToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Token = strings.TrimSpace(in.Token)
	if in.Token == "" {
		apiError(w, http.StatusBadRequest, "token is required")
		return
	}
	// Verify the token against GitHub BEFORE storing it, so a bad token never
	// overwrites a working clone token the agents depend on.
	var u struct {
		Login string `json:"login"`
	}
	if err := s.ghGetWithToken(r.Context(), in.Token, "/user", &u); err != nil {
		apiError(w, http.StatusBadRequest, "GitHub rejected the token: "+err.Error())
		return
	}
	if err := s.putSecret(r.Context(), cloneTokenSecretName, in.Token); err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "github.connect", "github", 0, u.Login)
	writeJSONAPI(w, http.StatusOK, map[string]any{"connected": true, "login": u.Login})
}

func (s *Server) apiGithubDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.db.WriteQ.DeleteSecret(r.Context(), cloneTokenSecretName); err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "github.disconnect", "github", 0, "")
	writeJSONAPI(w, http.StatusOK, map[string]any{"connected": false})
}

func (s *Server) apiGithubRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.ghListRepos(r.Context())
	if err != nil {
		apiError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]any, 0, len(repos))
	for _, rp := range repos {
		out = append(out, map[string]any{"full_name": rp.FullName, "private": rp.Private, "default_branch": rp.DefaultBranch})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiGithubBranches(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if !validRepo(repo) {
		apiError(w, http.StatusBadRequest, "repo must be owner/name")
		return
	}
	branches, err := s.ghListBranches(r.Context(), repo)
	if err != nil {
		apiError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]any, 0, len(branches))
	for _, b := range branches {
		out = append(out, map[string]any{"name": b.Name, "sha": b.Commit.SHA})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

func (s *Server) apiGithubCommits(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	branch := r.URL.Query().Get("branch")
	if !validRepo(repo) {
		apiError(w, http.StatusBadRequest, "repo must be owner/name")
		return
	}
	commits, err := s.ghListCommits(r.Context(), repo, branch)
	if err != nil {
		apiError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]any, 0, len(commits))
	for _, c := range commits {
		msg := c.Commit.Message
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		out = append(out, map[string]any{
			"sha": c.SHA, "message": msg,
			"author": c.Commit.Author.Name, "date": c.Commit.Author.Date,
		})
	}
	writeJSONAPI(w, http.StatusOK, out)
}

// validRepo checks a GitHub "owner/name" slug (no path traversal / injection).
func validRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, p := range parts {
		for _, c := range p {
			ok := c == '-' || c == '_' || c == '.' ||
				(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if !ok {
				return false
			}
		}
	}
	return true
}
