-- +goose Up
-- +goose StatementBegin
-- Per-app git state read off the bench's apps/<name>/.git by the agent's
-- discovery push. git_commit is the real HEAD SHA (distinct from current_commit,
-- which historically held the __version__ string); git_branch is the checked-out
-- branch; git_remote is the normalized owner/repo of the branch's tracking remote
-- (used to look up the latest commit on the remote).
ALTER TABLE frappe_apps ADD COLUMN git_commit TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE frappe_apps ADD COLUMN git_branch TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE frappe_apps ADD COLUMN git_remote TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE frappe_apps DROP COLUMN git_commit;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE frappe_apps DROP COLUMN git_branch;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE frappe_apps DROP COLUMN git_remote;
-- +goose StatementEnd
