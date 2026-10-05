package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// jobSeed is one job in a multi-step deploy.
type jobSeed struct {
	serverID int64
	typ      jobs.Type
	params   any
	lockKey  string
}

// createDeploy writes a deploy row and its ordered jobs (seq 0..n) atomically
// enough for the worker: the first ready job is promoted on the next pass. It
// returns the deploy id. trigger is ui|webhook|api|schedule.
func (s *Server) createDeploy(ctx context.Context, kind, targetType string, targetID int64, trigger, commit string, userID int64, seeds []jobSeed) (int64, error) {
	if len(seeds) == 0 {
		return 0, fmt.Errorf("deploy has no jobs")
	}
	dep, err := s.db.WriteQ.CreateDeploy(ctx, store.CreateDeployParams{
		Kind:       kind,
		TargetType: targetType,
		TargetID:   targetID,
		CommitSha:  nullStr(commit),
		Trigger:    trigger,
		UserID:     nullableUser(userID),
	})
	if err != nil {
		return 0, err
	}
	for i, seed := range seeds {
		paramsJSON, err := json.Marshal(seed.params)
		if err != nil {
			return 0, err
		}
		spec, _ := jobs.Lookup(seed.typ)
		maxAttempts := int64(1)
		if spec.MaxAttempts > 0 {
			maxAttempts = int64(spec.MaxAttempts)
		}
		if _, err := s.db.WriteQ.CreateJob(ctx, store.CreateJobParams{
			DeployID:    nullInt(dep.ID),
			Seq:         int64(i),
			ServerID:    seed.serverID,
			Type:        string(seed.typ),
			ParamsJson:  string(paramsJSON),
			LockKey:     nullStr(seed.lockKey),
			MaxAttempts: maxAttempts,
		}); err != nil {
			return 0, err
		}
	}
	return dep.ID, nil
}

func nullableUser(id int64) sql.NullInt64 {
	if id > 0 {
		return nullInt(id)
	}
	return sql.NullInt64{}
}

// deployFrappeAppForPush builds one deploy per bench that has the app, each a
// single deploy_frappe_app job locked on that bench. Returns the number of
// deploys created.
func (s *Server) deployFrappeAppForPush(ctx context.Context, src store.AppSource, commit string) (int, error) {
	benches, err := s.db.ReadQ.BenchesWithApp(ctx, nullInt(src.ID))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range benches {
		sites, err := s.db.ReadQ.SitesForBench(ctx, b.ID)
		if err != nil {
			return n, err
		}
		var domains []string
		for _, st := range sites {
			if st.Status == "active" {
				domains = append(domains, st.Domain)
			}
		}
		if len(domains) == 0 {
			continue
		}
		params := jobs.DeployFrappeAppParams{
			BenchPath: b.Path,
			App:       src.Name,
			Branch:    src.Branch,
			Commit:    commit,
			Sites:     domains,
			CloneTok:  jobs.SecretRef("secret:" + cloneTokenSecretName),
		}
		if _, err := s.createDeploy(ctx, "frappe_app", "bench", b.ID, "webhook", commit, 0, []jobSeed{
			{serverID: b.ServerID, typ: jobs.TypeDeployFrappeApp, params: params, lockKey: benchLock(b.ID)},
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// deployWebAppForPush builds one deploy per web app built from the source.
func (s *Server) deployWebAppForPush(ctx context.Context, src store.AppSource, commit string) (int, error) {
	webApps, err := s.db.ReadQ.WebAppsForSource(ctx, src.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, wa := range webApps {
		params := jobs.DeployWebAppParams{
			Name:          wa.Name,
			Repo:          src.Repo,
			Branch:        src.Branch,
			Commit:        commit,
			InternalPort:  int(wa.InternalPort),
			ContainerPort: int(wa.ContainerPort),
			HealthPath:    wa.HealthPath,
			Env:           jobs.SecretRef("secret:" + webAppEnvSecretName(wa.ID)),
			CloneTok:      jobs.SecretRef("secret:" + cloneTokenSecretName),
		}
		if _, err := s.createDeploy(ctx, "web_app", "web_app", wa.ID, "webhook", commit, 0, []jobSeed{
			{serverID: wa.ServerID, typ: jobs.TypeDeployWebApp, params: params, lockKey: webAppLock(wa.ID)},
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Lock key helpers.
func benchLock(id int64) string   { return fmt.Sprintf("bench:%d", id) }
func webAppLock(id int64) string  { return fmt.Sprintf("web_app:%d", id) }
func nginxLock(serverID int64) string { return fmt.Sprintf("nginx:%d", serverID) }

// Secret name helpers.
const cloneTokenSecretName = "github_clone_token"

func webAppEnvSecretName(id int64) string { return fmt.Sprintf("web_app_env:%d", id) }
func siteAdminSecretName(id int64) string { return fmt.Sprintf("site_admin:%d", id) }
func dbRootSecretName(benchID int64) string { return fmt.Sprintf("db_root:bench:%d", benchID) }
