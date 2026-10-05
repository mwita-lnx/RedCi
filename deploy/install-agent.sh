#!/usr/bin/env bash
# install-agent.sh — provision the RedCi agent as a bare-metal systemd service.
#
#   sudo AGENT_USER=myuser OPS_PANEL_URL=https://panel.internal ./install-agent.sh
#
# AGENT_USER defaults to the current (non-root) user so you can just:
#   sudo ./install-agent.sh
#
# Re-running is safe; each step is idempotent.
set -euo pipefail

PANEL_URL="${OPS_PANEL_URL:-https://panel.internal}"
AGENT_USER="${AGENT_USER:-${SUDO_USER:-$(logname 2>/dev/null || echo ops-agent)}}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

log() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }

[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }

log "agent will run as user: $AGENT_USER"

# Create the user if it doesn't already exist.
if ! id "$AGENT_USER" &>/dev/null; then
  log "creating system user $AGENT_USER"
  useradd --system --no-create-home --shell /usr/sbin/nologin "$AGENT_USER"
fi

log "nginx ops.d directory + include"
install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0755 /etc/nginx/ops.d
if ! grep -q 'include /etc/nginx/ops.d/\*.conf;' /etc/nginx/nginx.conf 2>/dev/null; then
  sed -i '0,/http\s*{/s//http {\n    include \/etc\/nginx\/ops.d\/*.conf;/' /etc/nginx/nginx.conf || \
    log "could not auto-edit nginx.conf; add 'include /etc/nginx/ops.d/*.conf;' inside http{} by hand"
fi
if [ ! -f /etc/nginx/ops.d/00-ops-common.conf ]; then
  cat > /etc/nginx/ops.d/00-ops-common.conf <<'EOF'
map $http_upgrade $connection_upgrade { default upgrade; '' close; }
EOF
  chown "$AGENT_USER":"$AGENT_USER" /etc/nginx/ops.d/00-ops-common.conf
fi

log "sudoers for the agent"
sed "s/AGENT_USER/$AGENT_USER/g" "$REPO_ROOT/deploy/sudoers.ops-agent" \
  > /etc/sudoers.d/ops-agent
chmod 0440 /etc/sudoers.d/ops-agent
visudo -cf /etc/sudoers.d/ops-agent

log "add $AGENT_USER to the docker group (for web-app jobs)"
getent group docker >/dev/null 2>&1 && usermod -aG docker "$AGENT_USER" || log "no docker group; skipping"

nginx -t && systemctl reload nginx || log "nginx -t failed; review before serving"

log "installing ops-agent binary"
if [ -x "$REPO_ROOT/bin/agent" ]; then
  install -m 0755 "$REPO_ROOT/bin/agent" /usr/local/bin/ops-agent
else
  echo "build it first: (cd $REPO_ROOT && make build)"; exit 1
fi

install -d -m 0755 /etc/ops-agent
install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0700 /var/lib/ops-agent
[ -f /etc/ops-agent/config.toml ] || install -m 0644 "$REPO_ROOT/deploy/agent-config.toml" /etc/ops-agent/config.toml

log "installing systemd unit"
sed "s/AGENT_USER/$AGENT_USER/g" "$REPO_ROOT/deploy/agent.service" \
  > /etc/systemd/system/ops-agent.service
systemctl daemon-reload

cat <<EOF

Install complete. Next:
  1. Edit /etc/ops-agent/config.toml (panel_url, bench_roots, bench_path_env).
  2. Enroll:  sudo -u $AGENT_USER ops-agent register --panel $PANEL_URL --token <token>
  3. Start:   sudo systemctl enable --now ops-agent
EOF
