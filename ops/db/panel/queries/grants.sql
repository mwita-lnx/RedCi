-- name: ListGrantsForUser :many
SELECT * FROM resource_grants WHERE user_id = ? ORDER BY resource_type, resource_id;

-- name: GrantsForUser :many
-- All grants for a user (used by the effective-role resolver). Same as above
-- but named for intent at the call site.
SELECT resource_type, resource_id, role FROM resource_grants WHERE user_id = ?;

-- name: UpsertGrant :one
-- Add or update a grant. Re-granting the same (user, resource) replaces the role.
INSERT INTO resource_grants (user_id, resource_type, resource_id, role)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id, resource_type, resource_id)
DO UPDATE SET role = excluded.role
RETURNING *;

-- name: DeleteGrant :exec
DELETE FROM resource_grants WHERE id = ? AND user_id = ?;

-- name: GetGrant :one
SELECT * FROM resource_grants WHERE id = ? LIMIT 1;
