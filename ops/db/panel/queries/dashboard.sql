-- name: CountServers :one
SELECT COUNT(*) FROM servers;

-- name: CountServersOnline :one
SELECT COUNT(*) FROM servers WHERE status = 'online';

-- name: CountSites :one
SELECT COUNT(*) FROM sites WHERE status != 'archived';

-- name: CountWebApps :one
SELECT COUNT(*) FROM web_apps;

-- name: CountRunningJobs :one
SELECT COUNT(*) FROM jobs WHERE status IN ('claimed','running');

-- name: ListServers :many
SELECT * FROM servers ORDER BY name;

-- name: CountServersOffline :one
SELECT COUNT(*) FROM servers WHERE status IN ('offline','disabled');

-- name: CountExpiringCerts :one
SELECT
  (SELECT COUNT(*) FROM sites si WHERE si.ssl_enabled = 1 AND si.ssl_expires_at IS NOT NULL AND si.ssl_expires_at < ?)
  + (SELECT COUNT(*) FROM proxy_routes pr WHERE pr.ssl_enabled = 1 AND pr.ssl_expires_at IS NOT NULL AND pr.ssl_expires_at < ?) AS expiring;
