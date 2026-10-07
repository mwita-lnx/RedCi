-- Pipelines (dev -> staging -> prod release trains) -------------------------

-- name: CreatePipeline :one
INSERT INTO pipelines (name, app_source_id)
VALUES (?, ?)
RETURNING *;

-- name: ListPipelines :many
SELECT * FROM pipelines ORDER BY name;

-- name: GetPipeline :one
SELECT * FROM pipelines WHERE id = ? LIMIT 1;

-- name: DeletePipeline :exec
DELETE FROM pipelines WHERE id = ?;

-- name: CreatePipelineEnv :one
INSERT INTO pipeline_envs (pipeline_id, name, rank, site_id, require_approval)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: DeletePipelineEnvs :exec
DELETE FROM pipeline_envs WHERE pipeline_id = ?;

-- name: GetPipelineEnvByRank :one
SELECT * FROM pipeline_envs WHERE pipeline_id = ? AND rank = ? LIMIT 1;

-- name: GetPipelineEnvByDeploy :one
SELECT * FROM pipeline_envs WHERE last_deploy_id = ? LIMIT 1;

-- name: SetEnvDeploy :exec
UPDATE pipeline_envs SET last_deploy_id = ? WHERE id = ?;

-- name: SetEnvCommit :exec
UPDATE pipeline_envs SET current_commit = ? WHERE id = ?;

-- name: ListEnvsForPipeline :many
-- Envs joined to their site + bench + server so the API can show which agent
-- each environment runs on, its domain, and its deploy status.
SELECT
  pe.id, pe.pipeline_id, pe.name, pe.rank, pe.site_id,
  pe.require_approval, pe.current_commit, pe.last_deploy_id,
  s.domain       AS site_domain,
  s.status       AS site_status,
  b.id           AS bench_id,
  b.path         AS bench_path,
  srv.id         AS server_id,
  srv.name       AS server_name,
  srv.status     AS server_status,
  d.status       AS deploy_status
FROM pipeline_envs pe
JOIN sites   s   ON s.id = pe.site_id
JOIN benches b   ON b.id = s.bench_id
JOIN servers srv ON srv.id = b.server_id
LEFT JOIN deploys d ON d.id = pe.last_deploy_id
WHERE pe.pipeline_id = ?
ORDER BY pe.rank;
