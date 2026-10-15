package server

import (
	"fmt"
	"net/http"
	"strings"

	agentbin "github.com/mwita-lnx/RedCi/ops/web/agentbin"
)

// Agent install endpoints. These are intentionally UNAUTHENTICATED: a brand-new
// server has no panel session. Security comes from the one-time enrollment
// token embedded in the install command — the binary + script are not secret.

// handleAgentScript serves the bootstrap installer run as:
//
//	curl -fsSL https://panel/agent.sh | sudo sh -s -- --token <token>
//
// It downloads the right-arch agent binary from this panel, installs a systemd
// service, registers with the token, and starts the agent.
func (s *Server) handleAgentScript(w http.ResponseWriter, r *http.Request) {
	base := s.cfg.BaseURL
	if base == "" {
		base = "http://" + r.Host
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, agentInstallScript(base))
}

// handleAgentDownload serves the embedded Linux agent binary for an arch.
func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	arch := r.PathValue("arch")
	switch arch {
	case "amd64", "x86_64":
		arch = "amd64"
	case "arm64", "aarch64":
		arch = "arm64"
	default:
		http.Error(w, "unsupported arch (use amd64 or arm64)", http.StatusNotFound)
		return
	}
	data, err := agentbin.FS.ReadFile("ops-agent-linux-" + arch)
	if err != nil || len(data) == 0 {
		http.Error(w, "agent binary not bundled in this build", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="ops-agent"`)
	_, _ = w.Write(data)
}

// agentInstallScript renders the POSIX-sh bootstrap installer.
func agentInstallScript(panelURL string) string {
	panelURL = strings.TrimRight(panelURL, "/")
	return strings.ReplaceAll(installScriptTemplate, "{{PANEL}}", panelURL)
}

const installScriptTemplate = `#!/bin/sh
# RedCi agent installer (Ubuntu/Linux). Usage:
#   curl -fsSL {{PANEL}}/agent.sh | sudo sh -s -- --token <enrollment-token>
#
# It auto-detects the Frappe bench user, that user's HOME, and a PATH that
# includes bench + the bench venv + node + the mariadb client, and writes ALL of
# them into the systemd unit so jobs (new-site, deploy, npm/pm2 web-app builds)
# work out of the box. Any value can be overridden with a flag.
set -eu

PANEL="{{PANEL}}"
TOKEN=""
AGENT_USER=""
BENCH_ROOTS=""
APPS_ROOT=""
WEBSITES_ROOT=""
HOME_DIR=""
PATH_ENV=""

while [ $# -gt 0 ]; do
  case "$1" in
    --token) TOKEN="$2"; shift 2 ;;
    --panel) PANEL="$2"; shift 2 ;;
    --user) AGENT_USER="$2"; shift 2 ;;
    --bench-roots) BENCH_ROOTS="$2"; shift 2 ;;
    --apps-root) APPS_ROOT="$2"; shift 2 ;;
    --websites-root) WEBSITES_ROOT="$2"; shift 2 ;;
    --home) HOME_DIR="$2"; shift 2 ;;
    --path-env) PATH_ENV="$2"; shift 2 ;;
    --labels) shift 2 ;;   # accepted but not used yet
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

[ -n "$TOKEN" ] || { echo "error: --token is required" >&2; exit 1; }
[ "$(id -u)" = 0 ] || { echo "error: run with sudo / as root" >&2; exit 1; }

# --- pick the agent user ---------------------------------------------------
# Default to the conventional Frappe bench user if it exists, else the invoking
# sudo user, else root.
if [ -z "$AGENT_USER" ]; then
  if id frappe >/dev/null 2>&1; then AGENT_USER=frappe
  elif [ -n "${SUDO_USER:-}" ]; then AGENT_USER="$SUDO_USER"
  else AGENT_USER=root
  fi
fi
AGENT_HOME="$(getent passwd "$AGENT_USER" | cut -d: -f6)"
[ -n "$AGENT_HOME" ] || AGENT_HOME="/home/$AGENT_USER"
[ -n "$HOME_DIR" ] || HOME_DIR="$AGENT_HOME"
echo "==> agent user: $AGENT_USER (home $HOME_DIR)"

# --- locate the bench ------------------------------------------------------
# If --bench-roots not given, look for a frappe-bench under the user's home or
# common locations (a dir containing sites/apps.txt).
if [ -z "$BENCH_ROOTS" ]; then
  for cand in "$AGENT_HOME/frappe-bench" "$AGENT_HOME"/*/frappe-bench /home/frappe/frappe-bench /srv/frappe-bench; do
    if [ -f "$cand/sites/apps.txt" ]; then BENCH_ROOTS="$cand"; break; fi
  done
fi
[ -n "$BENCH_ROOTS" ] && echo "==> bench root: $BENCH_ROOTS" || echo "==> no bench auto-detected (set later in /etc/ops-agent/config.toml)"
[ -n "$APPS_ROOT" ] || APPS_ROOT="/srv/ops/apps"
[ -n "$WEBSITES_ROOT" ] || WEBSITES_ROOT="/srv/ops/websites"

# --- build the step PATH ---------------------------------------------------
# bench CLI (~/.local/bin), the bench virtualenv (pip/python), node (nvm or
# system), and the mariadb client (needed by bench new-site). /usr/bin is also
# appended by the agent's built-in tail, but we include it for clarity.
if [ -z "$PATH_ENV" ]; then
  PATH_ENV="$HOME_DIR/.local/bin"
  [ -n "$BENCH_ROOTS" ] && PATH_ENV="$PATH_ENV:${BENCH_ROOTS%%,*}/env/bin"
  # newest nvm node bin, if present
  NVM_BIN="$(ls -d "$HOME_DIR"/.nvm/versions/node/*/bin 2>/dev/null | sort -V | tail -1 || true)"
  [ -n "$NVM_BIN" ] && PATH_ENV="$PATH_ENV:$NVM_BIN"
  PATH_ENV="$PATH_ENV:/usr/local/bin:/usr/bin"
fi
echo "==> step PATH: $PATH_ENV"

# --- detect arch -----------------------------------------------------------
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "error: unsupported arch $(uname -m)" >&2; exit 1 ;;
esac

echo "==> downloading ops-agent ($ARCH) from $PANEL"
curl -fsSL "$PANEL/agent/download/$ARCH" -o /usr/local/bin/ops-agent
chmod 0755 /usr/local/bin/ops-agent

echo "==> preparing directories"
install -d -m 0755 /etc/ops-agent
install -d -o "$AGENT_USER" -g "$AGENT_USER" -m 0700 /var/lib/ops-agent

echo "==> registering with the panel"
OPS_AGENT_CREDENTIAL=/etc/ops-agent/credential \
  /usr/local/bin/ops-agent register --panel "$PANEL" --token "$TOKEN"
chown "$AGENT_USER":"$AGENT_USER" /etc/ops-agent/credential 2>/dev/null || true

echo "==> installing systemd service"
cat > /etc/systemd/system/ops-agent.service <<UNIT
[Unit]
Description=RedCi Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$AGENT_USER
Environment=HOME=$HOME_DIR
Environment=OPS_PANEL_URL=$PANEL
Environment=OPS_AGENT_CREDENTIAL=/etc/ops-agent/credential
Environment=OPS_AGENT_HOME=$HOME_DIR
Environment=OPS_AGENT_APPS_ROOT=$APPS_ROOT
Environment=OPS_AGENT_WEBSITES_ROOT=$WEBSITES_ROOT
Environment=OPS_AGENT_BENCH_PATH_ENV=$PATH_ENV
$( [ -n "$BENCH_ROOTS" ] && echo "Environment=OPS_AGENT_BENCH_ROOTS=$BENCH_ROOTS" )
ExecStart=/usr/local/bin/ops-agent run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now ops-agent

echo "==> done. The agent is registered and running as $AGENT_USER."
echo "    Env baked into the unit: HOME, OPS_AGENT_HOME, APPS_ROOT, WEBSITES_ROOT, BENCH_PATH_ENV${BENCH_ROOTS:+, BENCH_ROOTS}."
echo "    Check status:  systemctl status ops-agent"
echo "    Adjust later:  edit /etc/systemd/system/ops-agent.service, then systemctl daemon-reload && systemctl restart ops-agent"
`
