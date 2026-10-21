-- +goose Up
-- Per-resource RBAC: a grant raises a user's role on a specific site or bench
-- above their global role (users.role). Grants never lower access — the
-- effective role on a resource is max(global role, matching grants). A bench
-- grant covers every site on that bench.

-- +goose StatementBegin
CREATE TABLE resource_grants (
  id            INTEGER PRIMARY KEY,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL CHECK (resource_type IN ('site','bench')),
  resource_id   INTEGER NOT NULL,          -- sites.id or benches.id
  role          TEXT NOT NULL CHECK (role IN ('viewer','deployer','admin')),
  created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
  UNIQUE (user_id, resource_type, resource_id)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX idx_resource_grants_user ON resource_grants(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE resource_grants;
-- +goose StatementEnd
