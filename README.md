# RedCi Ops Panel

A self-hosted control plane, written in Go, that creates Frappe sites, deploys
Frappe apps and Next.js containers, and manages nginx routes and SSL across
on-prem servers from one web UI — replacing manual SSH work (`bench new-site`,
editing nginx, running certbot, rebuilding containers) with an audited,
form-driven workflow.

The full design lives in **Ops Panel Build Specification**. This repo is the
implementation, built in the phases the spec defines.

## Status

**Phases 0–2 complete and verified.** An admin logs in with mandatory TOTP;
servers enroll and appear online; jobs flow panel → worker → agent → live logs →
result with secrets resolved over TLS and redacted from logs. All twelve job
types are implemented; GitHub pushes fan out to deploys; proxy routes apply
atomically with `nginx -t` and restore-on-failure; web apps build and deploy
with health-checked auto-rollback. The full agent↔panel loop and the custom-op
dispatch path both pass end-to-end integration tests under `-race`.

| Area | State |
| --- | --- |
| SQLite store, full schema + agent journal, `sqlc` + `templ` codegen, static binaries | ✅ |
| Secrets: AES-256-GCM (key-version byte), master-key loader, params resolver | ✅ |
| Auth: argon2id, scs sessions, mandatory TOTP, login lockout; middleware chain + roles + audit | ✅ |
| `shared/jobs`: all 12 job types, strict-regex `Validate()` + fuzz, step plans + golden tests, nginx template | ✅ |
| `shared/protocol`: agent↔protocol wire types, version, secret references | ✅ |
| Panel agent API, worker (leases/cancels/promote+locks/finish/offline/housekeeping), event bus + SSE | ✅ |
| Agent: enrollment, heartbeat, long-poll loop, journal + outbox, crash recovery, backoff | ✅ |
| Agent runner: process groups, SIGTERM→SIGKILL, from-scratch env, per-step timeouts, redaction, `AlwaysRun`, `--dry-run` | ✅ |
| Agent custom ops: write/delete proxy route (atomic swap + restore), deploy/rollback web app (health-check + auto-rollback), `server_status` discovery, `list_nginx_configs` | ✅ |
| Agent nginx manager: render, fsync+rename swap, `nginx -t`, backup/restore, drift hashes | ✅ |
| Jobs: `new_site`, `issue_certificate`, `install_app`, `get_frappe_app`, `deploy_frappe_app`, `backup_site`, `write`/`delete_proxy_route`, `deploy`/`rollback_web_app`, `server_status`, `list_nginx_configs` | ✅ |
| GitHub webhook: HMAC-SHA256 verify, delivery dedupe, ping, push → one deploy per target; clone token via git env | ✅ |
| Panel orchestration: new-site-with-SSL follow-up, backup, route apply (HTTP→cert→SSL), discovery import | ✅ |
| UI pages: Dashboard (+warning cards), Servers (+add/refresh/import/detail), Nginx-files browser, Sites (+New Site), App sources, Web apps, Routes, Users (+add/reset-2FA/disable) — RedCi design | ✅ |
| Alerts: Slack-compatible outgoing webhook on failed deploys | ✅ |
| `panel` CLI: `serve`, `migrate`, `user create`, `server add`; Panel Docker image (`ops/Dockerfile`); Agent bare-metal systemd install script | ✅ |
| Phase 3: Litestream restore rehearsal, site-backup shipping, zero-downtime blue/green, `docker_prune`, agent self-update, Prometheus metrics | ⬜ |

## Layout

```
RedCi/
├── ops/            # the panel (control plane)
│   ├── cmd/panel/            serve · migrate · user create
│   ├── cmd/ops-certbot/      validating certbot wrapper (Phase 1)
│   ├── internal/             config, server, handlers, auth, secrets, store, worker, events
│   ├── db/panel/             goose migrations + sqlc queries
│   └── web/                  templ components + static assets (css, htmx)
├── agent/          # the on-prem agent
│   ├── cmd/agent/            register · run · version
│   ├── internal/             config, client, runner, ops, nginx, journal
│   └── Dockerfile
├── shared/         # jobs + protocol — imported by BOTH so they agree on job shapes
├── deploy/         # systemd units, sudoers, compose, install script, litestream
├── Makefile · sqlc.yaml · go.mod
```

The panel and agent share one Go module and the `shared/` packages, which keeps
their job definitions and validation identical — a core requirement of the spec.

## Build and run (local dev)

Prerequisites: Go 1.22+ and the codegen tools (`make tools` installs `templ`,
`sqlc`, `goose`).

```bash
make generate          # sqlc + templ + (optional) tailwind
make build             # -> bin/panel, bin/agent, bin/ops-certbot

# One 32-byte master key, kept OUT of the database and repo:
openssl rand -hex 32 > master.key

export OPS_MASTER_KEY=./master.key OPS_DB=./dev.db OPS_DEV=1
export OPS_BASE_URL=http://127.0.0.1:8080 OPS_LISTEN=127.0.0.1:8080

./bin/panel migrate
./bin/panel user create --email you@example.com --role admin
./bin/panel serve      # http://127.0.0.1:8080
```

On first login you set a password, then scan the TOTP QR with any authenticator
app before any other page is reachable.

`OPS_DEV=1` relaxes the `Secure` cookie flag for plain-HTTP local dev. In
production the panel is VPN-only and always behind HTTPS (`OPS_DEV` unset).

## Running the panel as Docker

The panel exposes port 8080 and stores its SQLite database in a named volume.

```bash
# Build the image (from the repo root):
docker build -f ops/Dockerfile -t redci-panel:latest .

# Or with compose (generates master.key first):
openssl rand -hex 32 > deploy/master.key
OPS_BASE_URL=https://panel.internal docker compose -f deploy/panel-compose.yaml up -d
docker compose -f deploy/panel-compose.yaml exec panel panel migrate
docker compose -f deploy/panel-compose.yaml exec panel panel user create --email you@example.com --role admin
```

`deploy/panel-compose.yaml` binds to `127.0.0.1:8080` (put nginx or a VPN in
front), mounts the master key as a Docker secret, and persists the database in
a named volume. `OPS_BASE_URL` must match the public URL the browser uses (needed
for CSRF origin checks).

## Installing the agent (bare metal)

The agent runs as a **systemd service** directly on the managed server. It needs
direct access to bench, nginx, certbot, and Docker — a container can't provide
that.

```bash
# Build the binary:
make build           # -> bin/agent

# Copy to the server and install:
sudo OPS_PANEL_URL=https://panel.internal ./deploy/install-agent.sh
```

The script installs the binary to `/usr/local/bin/ops-agent`, drops the systemd
unit, configures the nginx `ops.d/` include, and sets up the sudoers file. Then:

```bash
sudo -u frappe ops-agent register --panel https://panel.internal --token <token>
sudo systemctl enable --now ops-agent
```

## Tests

```bash
go test ./ops/...      # secrets round-trip, argon2id, roles, TOTP
go vet ./ops/... ./agent/...
```

## Configuration

| Variable | Used by | Meaning |
| --- | --- | --- |
| `OPS_LISTEN` | panel | bind address (default `127.0.0.1:8080`) |
| `OPS_DB` | panel | SQLite path |
| `OPS_BASE_URL` | panel | public base URL; trusted origin for CSRF |
| `OPS_MASTER_KEY` | panel | master-key path (fallback; prod uses systemd `LoadCredential`) |
| `OPS_DEV` | panel | `1` relaxes the Secure cookie for local HTTP |
| `OPS_PANEL_URL` | agent | panel URL the agent polls |
| `OPS_AGENT_*` | agent | override any `config.toml` value |

## Security notes (Phase 0)

- Passwords: argon2id (64 MB, 3 iterations, parallelism 2).
- TOTP mandatory; enforced before any other page.
- Sessions: `HttpOnly`, `SameSite=Lax`, `Secure` (prod), idle 1 h / absolute 12 h, token rotates at login.
- Login lockout: 10 failures per email **or** IP in 15 min blocks for 15 min.
- CSRF: origin-based (`filippo.io/csrf`) — cross-origin posts are rejected.
- Strict CSP (`default-src 'self'`, `frame-ancestors 'none'`); assets served from the binary.
- Secrets sealed with AES-256-GCM; master key never stored in the DB.
- Every mutating request writes an `audit_log` row.
