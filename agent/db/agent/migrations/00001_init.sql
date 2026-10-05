-- +goose Up
-- +goose StatementBegin
CREATE TABLE journal (
  job_id           INTEGER PRIMARY KEY,
  type             TEXT NOT NULL,
  params_json      TEXT NOT NULL,
  status           TEXT NOT NULL,          -- running, succeeded, failed, interrupted
  step             TEXT,                   -- last step started
  started_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL,
  result_reported  INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE outbox (                      -- log lines and results not yet accepted by the panel
  id       INTEGER PRIMARY KEY,
  job_id   INTEGER NOT NULL,
  kind     TEXT NOT NULL,                  -- logs | result
  payload  TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS journal;
-- +goose StatementEnd
