package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mwita-lnx/RedCi/ops/internal/store"
	"github.com/mwita-lnx/RedCi/shared/jobs"
)

// PutSecretForTest seals and stores a secret by name. Exported for the
// secrets UI (Phase 2) and integration tests.
func (s *Server) PutSecretForTest(ctx context.Context, name, value string) error {
	return s.putSecret(ctx, name, value)
}

// CreateEnrollment adds a pending server and returns its one-time enrollment
// token (shown once in the UI). Only the token's sha256 is stored.
func (s *Server) CreateEnrollment(ctx context.Context, name, hostname string) (serverID int64, token string, err error) {
	token, hash := newToken()
	srv, err := s.db.WriteQ.CreateServer(ctx, store.CreateServerParams{
		Name:                 name,
		Hostname:             hostname,
		EnrollmentTokenHash:  nullStr(hash),
		EnrollmentExpiresAt:  nullInt(time.Now().Add(time.Hour).Unix()),
	})
	if err != nil {
		return 0, "", err
	}
	return srv.ID, token, nil
}

// EnqueueJob creates a single-job deploy and inserts the job as queued, then
// nudges the worker to promote it. params are the (reference-carrying) params.
func (s *Server) EnqueueJob(ctx context.Context, serverID int64, t jobs.Type, params any, lockKey string, w interface{ Nudge() }) (deployID, jobID int64, err error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return 0, 0, err
	}
	dep, err := s.db.WriteQ.CreateDeploy(ctx, store.CreateDeployParams{
		Kind:       string(t),
		TargetType: "server",
		TargetID:   serverID,
		Trigger:    "api",
	})
	if err != nil {
		return 0, 0, err
	}
	spec, _ := jobs.Lookup(t)
	maxAttempts := int64(1)
	if spec.MaxAttempts > 0 {
		maxAttempts = int64(spec.MaxAttempts)
	}
	job, err := s.db.WriteQ.CreateJob(ctx, store.CreateJobParams{
		DeployID:    nullInt(dep.ID),
		Seq:         0,
		ServerID:    serverID,
		Type:        string(t),
		ParamsJson:  string(paramsJSON),
		LockKey:     nullStr(lockKey),
		MaxAttempts: maxAttempts,
	})
	if err != nil {
		return 0, 0, err
	}
	if w != nil {
		w.Nudge()
	}
	return dep.ID, job.ID, nil
}
