#!/usr/bin/env bash
# Agent runtime bootstrap and long-lived ACP listener entrypoint.
#
# Reads environment variables:
#   SOURCE_REPO      - Optional repository to clone into the workspace
#   SOURCE_REVISION  - Optional Git revision (commit/branch/tag) to check out
#   ACP_PORT         - Port for the ACP listener (default: 55514)
#   ACP_SECRET       - Optional auth secret/token (default: lattice-acp-token)
#   WORKSPACE_DIR    - Workspace path (default: /workspace)
#   LOG_LEVEL        - Daemon log level (default: info)

set -euo pipefail

SOURCE_REPO="${SOURCE_REPO:-}"
SOURCE_REVISION="${SOURCE_REVISION:-}"
ACP_PORT="${ACP_PORT:-55514}"
ACP_SECRET="${ACP_SECRET:-}"
WORKSPACE_DIR="${WORKSPACE_DIR:-/workspace}"
LOG_LEVEL="${LOG_LEVEL:-info}"

if [[ ! "$ACP_PORT" =~ ^[0-9]+$ ]] || (( ACP_PORT < 1 || ACP_PORT > 65535 )); then
  echo "agent-bootstrap: ACP_PORT must be an integer between 1 and 65535" >&2
  exit 1
fi

# 1. Prepare workspace
mkdir -p "$WORKSPACE_DIR"
git config --global --add safe.directory "$WORKSPACE_DIR"
git config --global user.name "Lattice Agent"
git config --global user.email "agent@lattice.local"

if [ -n "$SOURCE_REPO" ]; then
  if [ ! -d "$WORKSPACE_DIR/.git" ]; then
    echo "agent-bootstrap: cloning $SOURCE_REPO into $WORKSPACE_DIR"
    git clone "$SOURCE_REPO" "$WORKSPACE_DIR"
  fi
  if [ -n "$SOURCE_REVISION" ]; then
    echo "agent-bootstrap: checking out $SOURCE_REVISION"
    git -C "$WORKSPACE_DIR" checkout "$SOURCE_REVISION"
  fi
else
  if [ ! -d "$WORKSPACE_DIR/.git" ]; then
    echo "agent-bootstrap: initializing empty repository in $WORKSPACE_DIR"
    git -C "$WORKSPACE_DIR" init
    touch "$WORKSPACE_DIR/.gitkeep"
    git -C "$WORKSPACE_DIR" add .gitkeep
    git -C "$WORKSPACE_DIR" commit -m "initial workspace commit" || true
  fi
fi

# 2. Configure ACP listener runtime environment
HYDRA_HOME="${HYDRA_ACP_HOME:-/run/hydra-acp}"
mkdir -p "$HYDRA_HOME" "$HYDRA_HOME/xdg"
export HYDRA_ACP_HOME="$HYDRA_HOME"
export XDG_CONFIG_HOME="$HYDRA_HOME/xdg"

TOKEN="${ACP_SECRET:-lattice-acp-token}"
# auth-token holds the service token hydra-acp validates against the WS
# subprotocol (hydra-acp-token.<token>) on EXTERNAL client connections only.
# Internal daemon<->agent (pi-acp) comms run over stdio pipes and do NOT use
# this token. Note: the default below is public — set ACP_SECRET explicitly
# whenever the endpoint is exposed to anything untrusted.
printf '%s\n' "$TOKEN" > "$HYDRA_HOME/auth-token"
chmod 0600 "$HYDRA_HOME/auth-token"

jq -n \
  --argjson port "$ACP_PORT" \
  --arg logLevel "$LOG_LEVEL" \
  --arg piAcpDir "$WORKSPACE_DIR/.pi-acp" \
  --arg path "$PATH:/run/current-system/sw/bin:/bin:/usr/bin" \
  --arg workspace "$WORKSPACE_DIR" \
  '{
    daemon: {
      host: "0.0.0.0",
      port: $port,
      logLevel: $logLevel,
      sessionIdleTimeoutSeconds: 0,
      nonInteractiveOrphanTimeoutSeconds: 0,
      scrubEnv: []
    },
    registry: { pinned: true },
    agents: {
      "pi-acp": {
        command: "pi-acp",
        args: [],
        env: {
          PI_ACP_DIR: $piAcpDir,
          PI_CODING_AGENT_DIR: "/root/.pi/agent",
          PATH: $path,
          SSL_CERT_FILE: "/etc/ssl/certs/ca-bundle.crt",
          LANG: "C.UTF-8",
          LC_ALL: "C.UTF-8"
        }
      }
    },
    defaultAgent: "pi-acp",
    defaultCwd: $workspace
  }' > "$HYDRA_HOME/config.json"

echo "agent-bootstrap: starting ACP listener on 0.0.0.0:$ACP_PORT (workspace: $WORKSPACE_DIR)"
exec hydra-acp-daemon
