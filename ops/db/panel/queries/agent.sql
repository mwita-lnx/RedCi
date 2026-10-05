-- Server enrollment and heartbeat -------------------------------------------

-- name: CreateServer :one
INSERT INTO servers (name, hostname, status, enrollment_token_hash, enrollment_expires_at)
VALUES (?, ?, 'pending', ?, ?)
RETURNING *;

-- name: GetServerByID :one
SELECT * FROM servers WHERE id = ? LIMIT 1;

-- name: FindServerByEnrollmentHash :one
SELECT * FROM servers
WHERE enrollment_token_hash = ? AND enrollment_expires_at > ?
LIMIT 1;

-- name: CompleteEnrollment :exec
UPDATE servers
SET status = 'online', agent_token_hash = ?, agent_version = ?, hostname = ?,
    facts_json = ?, last_heartbeat_at = unixepoch(),
    enrollment_token_hash = NULL, enrollment_expires_at = NULL
WHERE id = ?;

-- name: FindServerByAgentTokenHash :one
SELECT * FROM servers WHERE agent_token_hash = ? LIMIT 1;

-- name: Heartbeat :exec
UPDATE servers
SET status = 'online', agent_version = ?, facts_json = ?, last_heartbeat_at = unixepoch()
WHERE id = ?;

-- name: MarkServersOffline :exec
UPDATE servers
SET status = 'offline'
WHERE status = 'online' AND last_heartbeat_at < ?;

-- Job dispatch ---------------------------------------------------------------

-- name: ClaimNextJob :one
UPDATE jobs
SET status = 'claimed', claimed_at = unixepoch(),
    lease_expires_at = unixepoch() + 60, attempt = attempt + 1
WHERE jobs.id = (
  SELECT j.id FROM jobs j
  WHERE j.server_id = ? AND j.status = 'ready'
  ORDER BY j.created_at, j.id LIMIT 1
)
RETURNING jobs.id, jobs.type, jobs.params_json, jobs.server_id, jobs.deploy_id;

-- name: StartJob :exec
UPDATE jobs SET status = 'running', started_at = unixepoch()
WHERE id = ? AND status = 'claimed';

-- name: RenewLease :exec
UPDATE jobs SET lease_expires_at = unixepoch() + 60
WHERE id = ? AND status IN ('claimed','running');

-- name: FinishJob :exec
UPDATE jobs
SET status = ?, exit_code = ?, error = ?, result_json = ?, finished_at = unixepoch()
WHERE id = ?;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = ? LIMIT 1;

-- name: InsertJobLog :exec
INSERT INTO job_logs (job_id, seq, ts, stream, line)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (job_id, seq) DO NOTHING;

-- name: ListJobLogs :many
SELECT job_id, seq, ts, stream, line FROM job_logs
WHERE job_id = ? AND seq > ?
ORDER BY seq;

-- name: CancelJobsRequested :exec
UPDATE jobs SET status = 'cancelled', finished_at = unixepoch()
WHERE status IN ('queued','ready') AND cancel_requested = 1;

-- name: ListCancelRequestedForServer :many
SELECT id FROM jobs
WHERE server_id = ? AND cancel_requested = 1 AND status IN ('claimed','running');

-- Worker passes --------------------------------------------------------------

-- name: ExpireLeases :many
UPDATE jobs SET status = 'lost', finished_at = unixepoch()
WHERE status IN ('claimed','running') AND lease_expires_at < ?
RETURNING id, deploy_id, server_id;

-- name: ReadyJobsToPromote :many
-- Candidate queued jobs with no earlier unfinished sibling and a free lock.
SELECT j.id, j.server_id, j.lock_key, j.deploy_id, j.seq
FROM jobs j
WHERE j.status = 'queued'
ORDER BY j.deploy_id, j.seq, j.id;

-- name: EarlierSiblingsSucceeded :one
SELECT COUNT(*) AS not_done
FROM jobs
WHERE deploy_id = ? AND seq < ? AND status != 'succeeded';

-- name: LockHeld :one
SELECT COUNT(*) AS held
FROM jobs
WHERE lock_key = ? AND status IN ('ready','claimed','running');

-- name: PromoteJob :exec
UPDATE jobs SET status = 'ready' WHERE id = ? AND status = 'queued';

-- name: CreateJob :one
INSERT INTO jobs (deploy_id, seq, server_id, type, params_json, lock_key, max_attempts)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListJobsForDeploy :many
SELECT * FROM jobs WHERE deploy_id = ? ORDER BY seq, id;

-- name: LatestResultForType :one
SELECT result_json FROM jobs
WHERE server_id = ? AND type = ? AND status = 'succeeded'
ORDER BY finished_at DESC, id DESC
LIMIT 1;

-- Deploys --------------------------------------------------------------------

-- name: CreateDeploy :one
INSERT INTO deploys (kind, target_type, target_id, commit_sha, trigger, user_id)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: SetDeployRunning :exec
UPDATE deploys SET status = 'running' WHERE id = ? AND status = 'queued';

-- name: FinishDeploy :exec
UPDATE deploys SET status = ?, finished_at = unixepoch() WHERE id = ?;

-- name: GetDeploy :one
SELECT * FROM deploys WHERE id = ? LIMIT 1;

-- name: ListRecentDeploys :many
SELECT
  d.*,
  (SELECT j.id FROM jobs j WHERE j.deploy_id = d.id ORDER BY j.seq DESC LIMIT 1) AS last_job_id,
  (SELECT j.error FROM jobs j WHERE j.deploy_id = d.id AND j.error IS NOT NULL ORDER BY j.seq DESC LIMIT 1) AS last_error
FROM deploys d
ORDER BY d.created_at DESC LIMIT ?;

-- name: CountDeployJobsByStatus :one
SELECT
  COUNT(*) AS total,
  CAST(COALESCE(SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END), 0) AS INTEGER) AS succeeded,
  CAST(COALESCE(SUM(CASE WHEN status IN ('failed','lost','cancelled') THEN 1 ELSE 0 END), 0) AS INTEGER) AS bad
FROM jobs WHERE deploy_id = ?;
