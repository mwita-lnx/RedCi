-- +goose Up
-- +goose StatementBegin
-- Track the previous commit and the pre-deploy backup file so a frappe app
-- deploy can be rolled back to its last-known-good code + database.
ALTER TABLE frappe_apps ADD COLUMN previous_commit TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- Per-(bench,app) record of the most recent successful deploy's rollback point:
-- the commit we moved away from and the backup taken just before migrating.
-- One row per app; overwritten on each successful deploy.
CREATE TABLE frappe_deploy_points (
  id             INTEGER PRIMARY KEY,
  bench_id       INTEGER NOT NULL REFERENCES benches(id),
  app_name       TEXT NOT NULL,
  prev_commit    TEXT NOT NULL,
  backups_json   TEXT NOT NULL DEFAULT '{}',  -- {site: backup_db_path}
  created_at     INTEGER NOT NULL DEFAULT (unixepoch()),
  UNIQUE (bench_id, app_name)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS frappe_deploy_points;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE frappe_apps DROP COLUMN previous_commit;
-- +goose StatementEnd
