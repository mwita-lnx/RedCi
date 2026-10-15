-- +goose Up
-- +goose StatementBegin
-- The agent pushes a file-only bench/site discovery payload every 60s. We stash
-- the most recent one per server so the "Refresh" action can re-import on demand
-- without enqueuing a job (the server_status job type has been removed).
ALTER TABLE servers ADD COLUMN last_discovery_json TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE servers DROP COLUMN last_discovery_json;
-- +goose StatementEnd
