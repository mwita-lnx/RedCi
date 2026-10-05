-- name: GetSecret :one
SELECT value_enc FROM secrets WHERE name = ? LIMIT 1;

-- name: PutSecret :exec
INSERT INTO secrets (name, value_enc, updated_at)
VALUES (?, ?, unixepoch())
ON CONFLICT (name) DO UPDATE SET value_enc = excluded.value_enc, updated_at = unixepoch();

-- name: DeleteSecret :exec
DELETE FROM secrets WHERE name = ?;

-- name: RecordWebhookDelivery :exec
INSERT INTO webhook_deliveries (delivery_id, event) VALUES (?, ?)
ON CONFLICT (delivery_id) DO NOTHING;

-- name: GetWebhookDelivery :one
SELECT delivery_id FROM webhook_deliveries WHERE delivery_id = ? LIMIT 1;

-- name: PurgeOldWebhookDeliveries :exec
DELETE FROM webhook_deliveries WHERE received_at < ?;
