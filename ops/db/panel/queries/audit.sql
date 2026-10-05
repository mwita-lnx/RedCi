-- name: InsertAudit :exec
INSERT INTO audit_log (user_id, actor, action, target_type, target_id, detail_json, ip)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListAudit :many
SELECT * FROM audit_log
ORDER BY ts DESC
LIMIT ? OFFSET ?;

-- name: CountLoginAttempts :one
SELECT COUNT(*) FROM login_attempts
WHERE key = ? AND ts > ?;

-- name: InsertLoginAttempt :exec
INSERT INTO login_attempts (key) VALUES (?);

-- name: ClearLoginAttempts :exec
DELETE FROM login_attempts WHERE key = ?;

-- name: PurgeOldLoginAttempts :exec
DELETE FROM login_attempts WHERE ts < ?;
