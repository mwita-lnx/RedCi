-- +goose Up
-- +goose StatementBegin
-- A pipeline is a release train for one app source: it groups 2-3 environment
-- sites (dev -> staging -> prod) and promotes that app's commit through them.
CREATE TABLE pipelines (
  id             INTEGER PRIMARY KEY,
  name           TEXT NOT NULL UNIQUE,
  app_source_id  INTEGER NOT NULL REFERENCES app_sources(id),
  created_at     INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
-- Each environment maps to one site (which pins it to a bench -> server/agent).
-- rank orders the train (0=first/dev ... n=prod). current_commit is the last
-- successfully promoted commit; last_deploy_id links the most recent promotion.
CREATE TABLE pipeline_envs (
  id               INTEGER PRIMARY KEY,
  pipeline_id      INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
  name             TEXT NOT NULL,
  rank             INTEGER NOT NULL,
  site_id          INTEGER NOT NULL REFERENCES sites(id),
  require_approval INTEGER NOT NULL DEFAULT 1,
  current_commit   TEXT,
  last_deploy_id   INTEGER REFERENCES deploys(id),
  UNIQUE (pipeline_id, rank)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS pipeline_envs;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS pipelines;
-- +goose StatementEnd
