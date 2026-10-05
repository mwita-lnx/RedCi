-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
  id               INTEGER PRIMARY KEY,
  email            TEXT NOT NULL UNIQUE,
  name             TEXT NOT NULL,
  password_hash    TEXT NOT NULL,                 -- argon2id encoded string
  totp_secret_enc  BLOB,                          -- AES-GCM; NULL until 2FA enrolled
  role             TEXT NOT NULL CHECK (role IN ('admin','deployer','viewer')),
  disabled         INTEGER NOT NULL DEFAULT 0,
  created_at       INTEGER NOT NULL DEFAULT (unixepoch()),
  last_login_at    INTEGER
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE sessions (                          -- schema expected by scs sqlite3store
  token  TEXT PRIMARY KEY,
  data   BLOB NOT NULL,
  expiry REAL NOT NULL
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX sessions_expiry ON sessions(expiry);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE servers (
  id                     INTEGER PRIMARY KEY,
  name                   TEXT NOT NULL UNIQUE,
  hostname               TEXT NOT NULL,
  status                 TEXT NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending','online','offline','disabled')),
  enrollment_token_hash  TEXT,
  enrollment_expires_at  INTEGER,
  agent_token_hash       TEXT,
  agent_version          TEXT,
  facts_json             TEXT,
  last_heartbeat_at      INTEGER,
  created_at             INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE benches (
  id              INTEGER PRIMARY KEY,
  server_id       INTEGER NOT NULL REFERENCES servers(id),
  name            TEXT NOT NULL,
  path            TEXT NOT NULL,
  frappe_version  TEXT,
  created_at      INTEGER NOT NULL DEFAULT (unixepoch()),
  UNIQUE (server_id, path)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE app_sources (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  repo         TEXT NOT NULL,
  branch       TEXT NOT NULL DEFAULT 'main',
  kind         TEXT NOT NULL CHECK (kind IN ('frappe','web')),
  auto_deploy  INTEGER NOT NULL DEFAULT 1
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE frappe_apps (
  id              INTEGER PRIMARY KEY,
  bench_id        INTEGER NOT NULL REFERENCES benches(id),
  app_source_id   INTEGER REFERENCES app_sources(id),
  app_name        TEXT NOT NULL,
  current_commit  TEXT,
  UNIQUE (bench_id, app_name)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE sites (
  id              INTEGER PRIMARY KEY,
  bench_id        INTEGER NOT NULL REFERENCES benches(id),
  domain          TEXT NOT NULL UNIQUE,
  status          TEXT NOT NULL CHECK (status IN ('creating','active','failed','archived')),
  apps_json       TEXT NOT NULL DEFAULT '[]',
  ssl_enabled     INTEGER NOT NULL DEFAULT 0,
  ssl_expires_at  INTEGER,
  last_backup_at  INTEGER,
  created_at      INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE web_apps (
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
CREATE TABLE proxy_routes (
  id              INTEGER PRIMARY KEY,
  server_id       INTEGER NOT NULL REFERENCES servers(id),
  domain          TEXT NOT NULL,
  upstream        TEXT NOT NULL,
  web_app_id      INTEGER REFERENCES web_apps(id),
  ssl_enabled     INTEGER NOT NULL DEFAULT 1,
  ssl_expires_at  INTEGER,
  extra_snippet   TEXT,
  config_hash     TEXT,
  status          TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','active','failed','deleted')),
  UNIQUE (server_id, domain)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE deploys (
  id            INTEGER PRIMARY KEY,
  kind          TEXT NOT NULL,
  target_type   TEXT NOT NULL,
  target_id     INTEGER NOT NULL,
  commit_sha    TEXT,
  trigger       TEXT NOT NULL CHECK (trigger IN ('ui','webhook','api','schedule')),
  user_id       INTEGER REFERENCES users(id),
  status        TEXT NOT NULL DEFAULT 'queued'
                CHECK (status IN ('queued','running','succeeded','failed','cancelled')),
  created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
  finished_at   INTEGER
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE jobs (
  id                INTEGER PRIMARY KEY,
  deploy_id         INTEGER REFERENCES deploys(id),
  seq               INTEGER NOT NULL DEFAULT 0,
  server_id         INTEGER NOT NULL REFERENCES servers(id),
  type              TEXT NOT NULL,
  params_json       TEXT NOT NULL,
  lock_key          TEXT,
  status            TEXT NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued','ready','claimed','running',
                                      'succeeded','failed','cancelled','lost')),
  attempt           INTEGER NOT NULL DEFAULT 0,
  max_attempts      INTEGER NOT NULL DEFAULT 1,
  cancel_requested  INTEGER NOT NULL DEFAULT 0,
  lease_expires_at  INTEGER,
  claimed_at        INTEGER,
  started_at        INTEGER,
  finished_at       INTEGER,
  exit_code         INTEGER,
  error             TEXT,
  result_json       TEXT,
  created_at        INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX jobs_dispatch ON jobs(server_id, status, created_at);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX jobs_lock ON jobs(lock_key, status);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX jobs_deploy ON jobs(deploy_id, seq);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE job_logs (
  job_id  INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  seq     INTEGER NOT NULL,
  ts      INTEGER NOT NULL,
  stream  TEXT NOT NULL CHECK (stream IN ('stdout','stderr','system')),
  line    TEXT NOT NULL,
  PRIMARY KEY (job_id, seq)
) WITHOUT ROWID;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE audit_log (
  id           INTEGER PRIMARY KEY,
  ts           INTEGER NOT NULL DEFAULT (unixepoch()),
  user_id      INTEGER REFERENCES users(id),
  actor        TEXT NOT NULL,
  action       TEXT NOT NULL,
  target_type  TEXT,
  target_id    INTEGER,
  detail_json  TEXT,
  ip           TEXT
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX audit_ts ON audit_log(ts);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE secrets (
  name       TEXT PRIMARY KEY,
  value_enc  BLOB NOT NULL,
  updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE webhook_deliveries (
  delivery_id  TEXT PRIMARY KEY,
  event        TEXT NOT NULL,
  received_at  INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE login_attempts (                    -- backs the login-lockout policy
  id        INTEGER PRIMARY KEY,
  key       TEXT NOT NULL,                        -- lowercased email, or client IP
  ts        INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX login_attempts_key ON login_attempts(key, ts);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS job_logs;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS deploys;
DROP TABLE IF EXISTS proxy_routes;
DROP TABLE IF EXISTS web_apps;
DROP TABLE IF EXISTS sites;
DROP TABLE IF EXISTS frappe_apps;
DROP TABLE IF EXISTS app_sources;
DROP TABLE IF EXISTS benches;
DROP TABLE IF EXISTS servers;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
