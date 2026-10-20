-- +goose Up
-- Proxy routes can now be "unmanaged": discovered from an existing nginx vhost
-- (sites-enabled / conf.d) that RedCi did NOT write. Unmanaged routes are shown
-- read-only with their verbatim config; RedCi never re-renders or overwrites
-- them. `managed=1` (the default) keeps every existing route fully managed.
-- raw_config holds the discovered file's verbatim contents; nginx_path is where
-- it lives on the server (also the dedupe key for discovery).

-- +goose StatementBegin
ALTER TABLE proxy_routes ADD COLUMN managed INTEGER NOT NULL DEFAULT 1;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes ADD COLUMN raw_config TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes ADD COLUMN nginx_path TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE proxy_routes DROP COLUMN managed;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes DROP COLUMN raw_config;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes DROP COLUMN nginx_path;
-- +goose StatementEnd
