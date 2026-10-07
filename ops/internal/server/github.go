package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub integration (read-only). A Personal Access Token is stored encrypted
// under the existing github_clone_token secret (the same token the agent uses
// to clone), so saving it here also enables private-repo clones.

const githubAPI = "https://api.github.com"

// ghToken returns the stored PAT, or "" if none is set.
func (s *Server) ghToken(ctx context.Context) string {
	tok, err := s.openSecret(ctx, cloneTokenSecretName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(tok)
}

// ghGet calls the GitHub REST API with the stored token and decodes JSON into v.
func (s *Server) ghGet(ctx context.Context, path string, v any) error {
	tok := s.ghToken(ctx)
	if tok == "" {
		return fmt.Errorf("no GitHub token configured")
	}
	return s.ghGetWithToken(ctx, tok, path, v)
}

// ghGetWithToken is like ghGet but uses an explicit token (used to verify a
// token before storing it).
func (s *Server) ghGetWithToken(ctx context.Context, tok, path string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("GitHub rejected the token (401)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub API %s: %s", resp.Status, snippet(body))
	}
	if v != nil {
		return json.Unmarshal(body, v)
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

// --- typed GitHub responses (only the fields we use) ---

type ghRepo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

type ghBranch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type ghCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// ghListRepos lists repos the token can access (most recently pushed first).
func (s *Server) ghListRepos(ctx context.Context) ([]ghRepo, error) {
	var repos []ghRepo
	// affiliation covers owned + org + collaborator repos.
	err := s.ghGet(ctx, "/user/repos?per_page=100&sort=pushed&affiliation=owner,organization_member,collaborator", &repos)
	return repos, err
}

func (s *Server) ghListBranches(ctx context.Context, repo string) ([]ghBranch, error) {
	var out []ghBranch
	err := s.ghGet(ctx, fmt.Sprintf("/repos/%s/branches?per_page=100", repo), &out)
	return out, err
}

func (s *Server) ghListCommits(ctx context.Context, repo, branch string) ([]ghCommit, error) {
	var out []ghCommit
	q := url.Values{}
	q.Set("per_page", "20")
	if branch != "" {
		q.Set("sha", branch)
	}
	err := s.ghGet(ctx, fmt.Sprintf("/repos/%s/commits?%s", repo, q.Encode()), &out)
	return out, err
}

// ghLatestCommit returns the head commit SHA of a branch, or "" on any error
// (callers treat this as best-effort).
func (s *Server) ghLatestCommit(ctx context.Context, repo, branch string) string {
	if branch == "" {
		branch = "HEAD"
	}
	var c struct {
		SHA string `json:"sha"`
	}
	if err := s.ghGet(ctx, fmt.Sprintf("/repos/%s/commits/%s", repo, url.PathEscape(branch)), &c); err != nil {
		return ""
	}
	return c.SHA
}
