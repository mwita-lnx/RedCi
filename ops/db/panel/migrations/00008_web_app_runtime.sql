-- +goose Up
-- Web apps gain a runtime (docker|pm2) and carry their own git source (repo or
-- full URL + branch) so they can be created from the UI without first
-- registering an app_source. app_source_id becomes nullable (only set when the
-- app opts into webhook auto-deploy). SQLite can't drop a NOT NULL constraint in
-- place, so the table is rebuilt.

-- +goose StatementBegin
ALTER TABLE web_apps ADD COLUMN runtime TEXT NOT NULL DEFAULT 'docker'
  CHECK (runtime IN ('docker','pm2'));
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE web_apps ADD COLUMN repo TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE web_apps ADD COLUMN url TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE web_apps ADD COLUMN branch TEXT NOT NULL DEFAULT 'main';
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE web_apps ADD COLUMN auto_deploy INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- Rebuild to make app_source_id nullable (was NOT NULL).
-- +goose StatementBegin
CREATE TABLE web_apps_new (
  id               INTEGER PRIMARY KEY,
  server_id        INTEGER NOT NULL REFERENCES servers(id),
  app_source_id    INTEGER REFERENCES app_sources(id),
  name             TEXT NOT NULL UNIQUE,
  runtime          TEXT NOT NULL DEFAULT 'docker' CHECK (runtime IN ('docker','pm2')),
  repo             TEXT NOT NULL DEFAULT '',
  url              TEXT NOT NULL DEFAULT '',
  branch           TEXT NOT NULL DEFAULT 'main',
  auto_deploy      INTEGER NOT NULL DEFAULT 0,
  internal_port    INTEGER NOT NULL,
  container_port   INTEGER NOT NULL DEFAULT 3000,
  health_path      TEXT NOT NULL DEFAULT '/',
  env_enc          BLOB,
  current_commit   TEXT,
  previous_commit  TEXT,
  status           TEXT NOT NULL DEFAULT 'new'
                   CHECK (status IN ('new','deploying','running','failed','stopped')),
  UNIQUE (server_id, internal_port)
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO web_apps_new
  (id, server_id, app_source_id, name, runtime, repo, url, branch, auto_deploy,
   internal_port, container_port, health_path, env_enc, current_commit, previous_commit, status)
SELECT
  id, server_id, app_source_id, name, runtime, repo, url, branch, auto_deploy,
  internal_port, container_port, health_path, env_enc, current_commit, previous_commit, status
FROM web_apps;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE web_apps;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE web_apps_new RENAME TO web_apps;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE web_apps_old (
  id               INTEGER PRIMARY KEY,
  server_id        INTEGER NOT NULL REFERENCES servers(id),
  app_source_id    INTEGER NOT NULL REFERENCES app_sources(id),
  name             TEXT NOT NULL UNIQUE,
  internal_port    INTEGER NOT NULL,
  container_port   INTEGER NOT NULL DEFAULT 3000,
  health_path      TEXT NOT NULL DEFAULT '/',
  env_enc          BLOB,
  current_commit   TEXT,
  previous_commit  TEXT,
  status           TEXT NOT NULL DEFAULT 'new'
                   CHECK (status IN ('new','deploying','running','failed','stopped')),
  UNIQUE (server_id, internal_port)
);
-- +goose StatementEnd
-- +goose StatementBegin
INSERT INTO web_apps_old
  (id, server_id, app_source_id, name, internal_port, container_port, health_path,
   env_enc, current_commit, previous_commit, status)
SELECT id, server_id, app_source_id, name, internal_port, container_port, health_path,
   env_enc, current_commit, previous_commit, status
FROM web_apps WHERE app_source_id IS NOT NULL;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE web_apps;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE web_apps_old RENAME TO web_apps;
-- +goose StatementEnd
