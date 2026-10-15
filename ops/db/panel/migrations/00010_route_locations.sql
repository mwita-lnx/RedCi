-- +goose Up
-- +goose StatementBegin
-- Model B (frontend + Frappe path-split) support for proxy routes. When
-- locations_json is set, the agent renders a multi-location vhost instead of the
-- simple single-upstream proxy. frappe_site feeds X-Frappe-Site-Name on
-- frappe_proxy locations; static_root is the default filesystem root; aliases
-- are extra server_name hostnames. All nullable — existing simple routes leave
-- them empty and render exactly as before.
ALTER TABLE proxy_routes ADD COLUMN locations_json TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes ADD COLUMN frappe_site TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes ADD COLUMN static_root TEXT;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes ADD COLUMN aliases_json TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE proxy_routes DROP COLUMN locations_json;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes DROP COLUMN frappe_site;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes DROP COLUMN static_root;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE proxy_routes DROP COLUMN aliases_json;
-- +goose StatementEnd
