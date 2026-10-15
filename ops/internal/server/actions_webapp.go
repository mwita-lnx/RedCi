package server

import (
	"context"
	"fmt"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// WebAppSpec is the input for creating a web app from the UI.
type WebAppSpec struct {
	ServerID      int64
	Name          string
	Runtime       string // docker | pm2
	Repo          string // owner/name (optional if URL set)
	URL           string // full git clone URL (optional if Repo set)
	Branch        string
	InternalPort   int
	ContainerPort  int
	HealthPath     string
	DockerfilePath string // docker: Dockerfile path relative to repo root ("" = repo root)
	StartCommand   string // pm2
	BuildCommand   string // pm2
	AutoDeploy     bool
	Env            string // raw .env contents (stored encrypted)
}

// webAppDeployParams builds the deploy params from a stored web app + commit.
func (s *Server) webAppDeployParams(wa store.WebApp, commit string) jobs.DeployWebAppParams {
	return jobs.DeployWebAppParams{
		Name:          wa.Name,
		Runtime:       jobs.WebRuntime(wa.Runtime),
		Repo:          wa.Repo,
		URL:           wa.Url,
		Branch:        wa.Branch,
		Commit:        commit,
		InternalPort:   int(wa.InternalPort),
		ContainerPort:  int(wa.ContainerPort),
		HealthPath:     wa.HealthPath,
		WebsitesRoot:   "", // agent uses its configured websites root
		DockerfilePath: wa.DockerfilePath,
		Env:            jobs.SecretRef("secret:" + webAppEnvSecretName(wa.ID)),
		CloneTok:       jobs.SecretRef("secret:" + cloneTokenSecretName),
	}
}

// CreateWebApp stores a new web app (and its env secret). It does not deploy;
// the caller typically follows with DeployWebApp.
func (s *Server) CreateWebApp(ctx context.Context, in WebAppSpec) (int64, error) {
	if err := jobs.ValidateName(in.Name); err != nil {
		return 0, err
	}
	runtime := in.Runtime
	if runtime == "" {
		runtime = string(jobs.WebRuntimeDocker)
	}
	if runtime != string(jobs.WebRuntimeDocker) && runtime != string(jobs.WebRuntimePM2) {
		return 0, fmt.Errorf("runtime must be docker or pm2")
	}
	if in.URL != "" {
		if err := jobs.ValidateGitURL(in.URL); err != nil {
			return 0, err
		}
	} else if err := validateWebRepo(in.Repo); err != nil {
		return 0, err
	}
	if err := jobs.ValidatePort(in.InternalPort); err != nil {
		return 0, err
	}
	if in.ContainerPort != 0 {
		if err := jobs.ValidatePort(in.ContainerPort); err != nil {
			return 0, err
		}
	}

	containerPort := int64(in.ContainerPort)
	if containerPort == 0 {
		containerPort = 3000
	}
	healthPath := in.HealthPath
	if healthPath == "" {
		healthPath = "/"
	}

	wa, err := s.db.WriteQ.CreateWebApp(ctx, store.CreateWebAppParams{
		ServerID:      in.ServerID,
		Name:          in.Name,
		Runtime:       runtime,
		Repo:          in.Repo,
		Url:           in.URL,
		Branch:        branchOr(in.Branch),
		AutoDeploy:    boolToInt(in.AutoDeploy),
		InternalPort:   int64(in.InternalPort),
		ContainerPort:  containerPort,
		HealthPath:     healthPath,
		DockerfilePath: in.DockerfilePath,
		EnvEnc:         nil,
	})
	if err != nil {
		return 0, err
	}
	// Always store the .env secret (even when empty) so the deploy params'
	// "secret:web_app_env:<id>" reference always resolves — a missing secret
	// would otherwise fail the whole deploy at resolution time.
	if err := s.putSecret(ctx, webAppEnvSecretName(wa.ID), in.Env); err != nil {
		return 0, err
	}
	s.auditSystemCtx(ctx, "web_app.create", "web_app", wa.ID, in.Name+" ("+runtime+")")
	return wa.ID, nil
}

// DeployWebApp enqueues a deploy for an existing web app at the given commit
// (empty commit = latest on the app's branch via GitHub when resolvable).
func (s *Server) DeployWebApp(ctx context.Context, webAppID int64, commit string, startCmd, buildCmd string, userID int64) (int64, error) {
	wa, err := s.db.ReadQ.GetWebApp(ctx, webAppID)
	if err != nil {
		return 0, fmt.Errorf("web app %d: %w", webAppID, err)
	}
	if commit == "" {
		repo := wa.Repo
		if repo == "" {
			return 0, fmt.Errorf("a commit is required (no GitHub repo to resolve latest from)")
		}
		commit = s.ghLatestCommit(ctx, repo, wa.Branch)
		if commit == "" {
			return 0, fmt.Errorf("could not resolve latest commit for %s@%s", repo, wa.Branch)
		}
	}
	params := s.webAppDeployParams(wa, commit)
	// pm2 extras from the caller (optional overrides).
	params.StartCommand = startCmd
	params.BuildCommand = buildCmd

	depID, err := s.createDeploy(ctx, "web_app", "web_app", wa.ID, "ui", commit, userID, []jobSeed{
		{serverID: wa.ServerID, typ: jobs.TypeDeployWebApp, params: params, lockKey: webAppLock(wa.ID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// RollbackWebApp restores the web app's previous_commit.
func (s *Server) RollbackWebApp(ctx context.Context, webAppID int64, userID int64) (int64, error) {
	wa, err := s.db.ReadQ.GetWebApp(ctx, webAppID)
	if err != nil {
		return 0, fmt.Errorf("web app %d: %w", webAppID, err)
	}
	if !wa.PreviousCommit.Valid || wa.PreviousCommit.String == "" {
		return 0, fmt.Errorf("no previous commit recorded to roll back to")
	}
	params := jobs.RollbackWebAppParams{
		Name:          wa.Name,
		Runtime:       jobs.WebRuntime(wa.Runtime),
		Commit:        wa.PreviousCommit.String,
		InternalPort:  int(wa.InternalPort),
		ContainerPort: int(wa.ContainerPort),
		HealthPath:    wa.HealthPath,
	}
	depID, err := s.createDeploy(ctx, "web_app_rollback", "web_app", wa.ID, "ui", wa.PreviousCommit.String, userID, []jobSeed{
		{serverID: wa.ServerID, typ: jobs.TypeRollbackWebApp, params: params, lockKey: webAppLock(wa.ID)},
	})
	if err != nil {
		return 0, err
	}
	s.nudge()
	return depID, nil
}

// DeleteWebApp removes the web app record and its env secret. It does not tear
// down the running container/process on the server (left to the operator).
func (s *Server) DeleteWebApp(ctx context.Context, webAppID int64) error {
	wa, err := s.db.ReadQ.GetWebApp(ctx, webAppID)
	if err != nil {
		return fmt.Errorf("web app %d: %w", webAppID, err)
	}
	if err := s.db.WriteQ.DeleteWebApp(ctx, webAppID); err != nil {
		return err
	}
	_ = s.db.WriteQ.DeleteSecret(ctx, webAppEnvSecretName(webAppID))
	s.auditSystemCtx(ctx, "web_app.delete", "web_app", webAppID, wa.Name)
	return nil
}

func branchOr(b string) string {
	if b == "" {
		return "main"
	}
	return b
}

// validateWebRepo accepts an owner/name repo for a web app source.
func validateWebRepo(repo string) error {
	if repo == "" {
		return fmt.Errorf("a repo (owner/name) or a git URL is required")
	}
	if !validRepo(repo) {
		return fmt.Errorf("repo %q: invalid (want owner/name)", repo)
	}
	return nil
}
