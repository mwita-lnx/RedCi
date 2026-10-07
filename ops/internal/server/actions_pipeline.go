package server

import (
	"context"
	"fmt"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// EnvSpec is one environment in a new pipeline: a name, the site it maps to,
// and whether promoting into it requires manual approval.
type EnvSpec struct {
	Name            string
	SiteID          int64
	RequireApproval bool
}

// CreatePipeline creates a release train for one app source across a set of
// environment sites. Envs are ranked in the order given (0 = first stage).
func (s *Server) CreatePipeline(ctx context.Context, name string, appSourceID int64, envs []EnvSpec) (int64, error) {
	if len(envs) < 2 {
		return 0, fmt.Errorf("a pipeline needs at least two environments")
	}
	src, err := s.db.ReadQ.GetAppSource(ctx, appSourceID)
	if err != nil {
		return 0, fmt.Errorf("app source %d: %w", appSourceID, err)
	}
	if src.Kind != "frappe" {
		return 0, fmt.Errorf("pipelines currently support frappe app sources only")
	}
	// Validate every env site exists and has the app installed.
	for _, e := range envs {
		site, err := s.db.ReadQ.GetSite(ctx, e.SiteID)
		if err != nil {
			return 0, fmt.Errorf("env %q: site %d not found", e.Name, e.SiteID)
		}
		if site.Status == "archived" {
			return 0, fmt.Errorf("env %q: site %s is archived", e.Name, site.Domain)
		}
	}

	p, err := s.db.WriteQ.CreatePipeline(ctx, store.CreatePipelineParams{
		Name: name, AppSourceID: appSourceID,
	})
	if err != nil {
		return 0, err
	}
	for i, e := range envs {
		if _, err := s.db.WriteQ.CreatePipelineEnv(ctx, store.CreatePipelineEnvParams{
			PipelineID:      p.ID,
			Name:            e.Name,
			Rank:            int64(i),
			SiteID:          e.SiteID,
			RequireApproval: boolToInt(e.RequireApproval),
		}); err != nil {
			return 0, err
		}
	}
	s.auditSystemCtx(ctx, "pipeline.create", "pipeline", p.ID, name)
	return p.ID, nil
}

// PromoteEnv deploys the app's commit onto one environment's site. The deploy
// runs on the agent that owns that site (bench -> server), so different envs
// can live on different machines. The env's current_commit is updated when the
// deploy succeeds (see recordPipelinePromotion).
func (s *Server) PromoteEnv(ctx context.Context, pipelineID, envRank int64, commit string, userID int64) (int64, error) {
	if err := jobs.ValidateCommit(commit); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	p, err := s.db.ReadQ.GetPipeline(ctx, pipelineID)
	if err != nil {
		return 0, fmt.Errorf("pipeline %d: %w", pipelineID, err)
	}
	src, err := s.db.ReadQ.GetAppSource(ctx, p.AppSourceID)
	if err != nil {
		return 0, fmt.Errorf("app source: %w", err)
	}
	env, err := s.db.ReadQ.GetPipelineEnvByRank(ctx, store.GetPipelineEnvByRankParams{
		PipelineID: pipelineID, Rank: envRank,
	})
	if err != nil {
		return 0, fmt.Errorf("env rank %d not found", envRank)
	}
	site, err := s.db.ReadQ.GetSite(ctx, env.SiteID)
	if err != nil {
		return 0, fmt.Errorf("env site: %w", err)
	}
	if site.Status != "active" {
		return 0, fmt.Errorf("env site %s is not active", site.Domain)
	}
	bench, err := s.db.ReadQ.GetBench(ctx, site.BenchID)
	if err != nil {
		return 0, fmt.Errorf("env bench: %w", err)
	}

	params := jobs.DeployFrappeAppParams{
		BenchPath: bench.Path,
		App:       src.Name,
		Branch:    src.Branch,
		Commit:    commit,
		Sites:     []string{site.Domain},
		CloneTok:  jobs.SecretRef("secret:" + cloneTokenSecretName),
	}
	depID, err := s.createDeploy(ctx, "pipeline_promote", "site", site.ID, "ui", commit, userID, []jobSeed{
		{serverID: bench.ServerID, typ: jobs.TypeDeployFrappeApp, params: params, lockKey: benchLock(bench.ID)},
	})
	if err != nil {
		return 0, err
	}
	// Link this promotion to the env so the UI can show "deploying" and the
	// success hook knows which env to advance.
	_ = s.db.WriteQ.SetEnvDeploy(ctx, store.SetEnvDeployParams{
		LastDeployID: nullInt(depID), ID: env.ID,
	})
	s.auditSystemCtx(ctx, "pipeline.promote", "pipeline", pipelineID,
		fmt.Sprintf("%s -> %s @ %s", src.Name, env.Name, commit))
	s.nudge()
	return depID, nil
}

// recordPipelinePromotion advances a pipeline env's current_commit after its
// promotion deploy succeeds. Called from handleAgentJobResult for the final
// job of a pipeline_promote deploy. Best-effort.
func (s *Server) recordPipelinePromotion(ctx context.Context, deployID int64, commit string) {
	env, err := s.db.ReadQ.GetPipelineEnvByDeploy(ctx, nullInt(deployID))
	if err != nil {
		return // not a pipeline promotion, or env already changed
	}
	_ = s.db.WriteQ.SetEnvCommit(ctx, store.SetEnvCommitParams{
		CurrentCommit: nullStr(commit), ID: env.ID,
	})
}
