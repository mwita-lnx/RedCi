-- App sources ----------------------------------------------------------------

-- name: CreateAppSource :one
INSERT INTO app_sources (name, repo, branch, kind, auto_deploy)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAppSources :many
SELECT * FROM app_sources ORDER BY name;

-- name: GetAppSource :one
SELECT * FROM app_sources WHERE id = ? LIMIT 1;

-- name: GetAppSourceByName :one
SELECT * FROM app_sources WHERE name = ? LIMIT 1;

-- name: FindAppSourcesByRepoBranch :many
SELECT * FROM app_sources WHERE repo = ? AND branch = ? AND auto_deploy = 1;

-- name: SetAppSourceAutoDeploy :exec
UPDATE app_sources SET auto_deploy = ? WHERE id = ?;

-- Benches / frappe apps / sites ----------------------------------------------

-- name: CreateBench :one
INSERT INTO benches (server_id, name, path, frappe_version)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListBenches :many
SELECT * FROM benches ORDER BY server_id, path;

-- name: ListBenchesForServer :many
SELECT * FROM benches WHERE server_id = ? ORDER BY path;

-- name: GetBench :one
SELECT * FROM benches WHERE id = ? LIMIT 1;

-- name: SetBenchFacts :exec
UPDATE benches SET facts_json = ?, frappe_version = ? WHERE id = ?;

-- name: CreateFrappeApp :one
INSERT INTO frappe_apps (bench_id, app_source_id, app_name, current_commit)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListFrappeAppsForBench :many
SELECT * FROM frappe_apps WHERE bench_id = ? ORDER BY app_name;

-- name: BenchesWithApp :many
SELECT b.* FROM benches b
JOIN frappe_apps fa ON fa.bench_id = b.id
WHERE fa.app_source_id = ?;

-- name: SetFrappeAppCommit :exec
UPDATE frappe_apps SET current_commit = ? WHERE bench_id = ? AND app_name = ?;

-- name: SetFrappeAppCommits :exec
UPDATE frappe_apps SET previous_commit = ?, current_commit = ?
WHERE bench_id = ? AND app_name = ?;

-- name: UpsertDeployPoint :exec
INSERT INTO frappe_deploy_points (bench_id, app_name, prev_commit, backups_json)
VALUES (?, ?, ?, ?)
ON CONFLICT (bench_id, app_name)
DO UPDATE SET prev_commit = excluded.prev_commit,
              backups_json = excluded.backups_json,
              created_at = unixepoch();

-- name: GetDeployPoint :one
SELECT * FROM frappe_deploy_points WHERE bench_id = ? AND app_name = ? LIMIT 1;

-- name: CreateSite :one
INSERT INTO sites (bench_id, domain, status, apps_json, ssl_enabled)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListSites :many
SELECT * FROM sites ORDER BY domain;

-- name: GetSite :one
SELECT * FROM sites WHERE id = ? LIMIT 1;

-- name: GetSiteByDomain :one
SELECT * FROM sites WHERE domain = ? LIMIT 1;

-- name: SetSiteStatus :exec
UPDATE sites SET status = ? WHERE id = ?;

-- name: UpdateSiteApps :exec
UPDATE sites SET apps_json = ? WHERE id = ?;

-- name: ArchiveSite :exec
UPDATE sites SET status = 'archived' WHERE id = ?;

-- name: DeleteSiteRow :exec
DELETE FROM sites WHERE id = ?;

-- name: SetSiteSSL :exec
UPDATE sites SET ssl_enabled = ?, ssl_expires_at = ? WHERE id = ?;

-- name: SitesForBench :many
SELECT * FROM sites WHERE bench_id = ? ORDER BY domain;

-- Web apps -------------------------------------------------------------------

-- name: CreateWebApp :one
INSERT INTO web_apps (server_id, app_source_id, name, internal_port, container_port, health_path, env_enc, status)
VALUES (?, ?, ?, ?, ?, ?, ?, 'new')
RETURNING *;

-- name: ListWebApps :many
SELECT * FROM web_apps ORDER BY name;

-- name: GetWebApp :one
SELECT * FROM web_apps WHERE id = ? LIMIT 1;

-- name: GetWebAppByName :one
SELECT * FROM web_apps WHERE name = ? LIMIT 1;

-- name: WebAppsForSource :many
SELECT * FROM web_apps WHERE app_source_id = ?;

-- name: SetWebAppStatus :exec
UPDATE web_apps SET status = ? WHERE id = ?;

-- name: SetWebAppCommit :exec
UPDATE web_apps SET current_commit = ?, previous_commit = ?, status = 'running' WHERE id = ?;

-- Proxy routes ---------------------------------------------------------------

-- name: CreateProxyRoute :one
INSERT INTO proxy_routes (server_id, domain, upstream, web_app_id, ssl_enabled, extra_snippet, status)
VALUES (?, ?, ?, ?, ?, ?, 'pending')
RETURNING *;

-- name: ListProxyRoutes :many
SELECT * FROM proxy_routes ORDER BY domain;

-- name: ListProxyRoutesForServer :many
SELECT * FROM proxy_routes WHERE server_id = ? ORDER BY domain;

-- name: GetProxyRoute :one
SELECT * FROM proxy_routes WHERE id = ? LIMIT 1;

-- name: SetProxyRouteApplied :exec
UPDATE proxy_routes SET status = 'active', config_hash = ? WHERE id = ?;

-- name: SetProxyRouteStatus :exec
UPDATE proxy_routes SET status = ? WHERE id = ?;

-- name: RouteOrSiteExistsForDomain :one
SELECT
  (SELECT COUNT(*) FROM proxy_routes pr WHERE pr.server_id = ? AND pr.domain = ? AND pr.status != 'deleted')
  + (SELECT COUNT(*) FROM sites s WHERE s.domain = ?) AS conflicts;
