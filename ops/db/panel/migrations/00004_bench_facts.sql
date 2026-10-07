-- +goose Up
-- +goose StatementBegin
-- Runtime facts discovered by server_status (python/node/db versions, worker
-- topology, scheduler, queue depths) for the Benches & sites overview.
ALTER TABLE benches ADD COLUMN facts_json TEXT NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE benches DROP COLUMN facts_json;
-- +goose StatementEnd
