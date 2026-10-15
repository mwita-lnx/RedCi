-- +goose Up
-- Docker-runtime web apps can point at a Dockerfile other than the repo-root
-- "Dockerfile" (e.g. monorepo apps/web/Dockerfile). Empty = repo-root Dockerfile.
-- +goose StatementBegin
ALTER TABLE web_apps ADD COLUMN dockerfile_path TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE web_apps DROP COLUMN dockerfile_path;
-- +goose StatementEnd
