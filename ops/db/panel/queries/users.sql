-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ? LIMIT 1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ? LIMIT 1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at;

-- name: CreateUser :one
INSERT INTO users (email, name, password_hash, role)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: SetUserTOTP :exec
UPDATE users SET totp_secret_enc = ? WHERE id = ?;

-- name: ClearUserTOTP :exec
UPDATE users SET totp_secret_enc = NULL WHERE id = ?;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = ? WHERE id = ?;

-- name: SetUserDisabled :exec
UPDATE users SET disabled = ? WHERE id = ?;

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = unixepoch() WHERE id = ?;
