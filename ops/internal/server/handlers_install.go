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
# RedCi agent installer. Usage:
#   curl -fsSL {{PANEL}}/agent.sh | sudo sh -s -- --token <enrollment-token>
set -eu

PANEL="{{PANEL}}"
TOKEN=""
BENCH_ROOTS=""
AGENT_USER="${SUDO_USER:-root}"

while [ $# -gt 0 ]; do
  case "$1" in
    --token) TOKEN="$2"; shift 2 ;;
    --panel) PANEL="$2"; shift 2 ;;
    --bench-roots) BENCH_ROOTS="$2"; shift 2 ;;
    --user) AGENT_USER="$2"; shift 2 ;;
    --labels) shift 2 ;;   # accepted but not used yet
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

[ -n "$TOKEN" ] || { echo "error: --token is required" >&2; exit 1; }
[ "$(id -u)" = 0 ] || { echo "error: run with sudo / as root" >&2; exit 1; }

# Detect arch.
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
install -d -m 0700 /var/lib/ops-agent

echo "==> registering with the panel"
OPS_AGENT_CREDENTIAL=/etc/ops-agent/credential \
  /usr/local/bin/ops-agent register --panel "$PANEL" --token "$TOKEN"

echo "==> installing systemd service"
cat > /etc/systemd/system/ops-agent.service <<UNIT
[Unit]
Description=RedCi Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=OPS_PANEL_URL=$PANEL
Environment=OPS_AGENT_CREDENTIAL=/etc/ops-agent/credential
$( [ -n "$BENCH_ROOTS" ] && echo "Environment=OPS_AGENT_BENCH_ROOTS=$BENCH_ROOTS" )
ExecStart=/usr/local/bin/ops-agent run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now ops-agent

echo "==> done. The agent is registered and running."
echo "    Check status:  systemctl status ops-agent"
`
