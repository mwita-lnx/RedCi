-- +goose Up
-- +goose StatementBegin
-- A job may need to hold two exclusive locks at once. The folded Press-style
-- new_site job, for example, touches both its bench AND the shared nginx config,
-- so it must serialize against other bench work (lock_key) and other nginx work
-- such as proxy routes and cert issuance (lock_key2). Nullable; existing jobs
-- and all single-lock jobs leave it empty.
ALTER TABLE jobs ADD COLUMN lock_key2 TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX jobs_lock2 ON jobs(lock_key2, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX jobs_lock2;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE jobs DROP COLUMN lock_key2;
-- +goose StatementEnd
