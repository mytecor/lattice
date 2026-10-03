{ nixpkgs, pkgs }:

let
  agentImage = pkgs.lattice.agent-image;
  bootstrapPkg = agentImage.passthru.bootstrap;
  publishPkg = pkgs.lattice.publish-agent-image;
  pullPkg = pkgs.lattice.pull-agent-image;
in
# 1. Image name matches target
assert agentImage.name == "lattice-agent-runtime.tar.gz";
# 2. Verify bootstrap script and distribution tooling contracts
pkgs.runCommand "agent-image-evaluation" {
  nativeBuildInputs = [ pkgs.bash pkgs.git pkgs.jq ];
} ''
  test -f "${bootstrapPkg}/bin/agent-runtime-bootstrap" || { echo "bootstrap script missing" >&2; exit 1; }
  BOOTSTRAP="${bootstrapPkg}/bin/agent-runtime-bootstrap"
  grep -q "SOURCE_REPO" "$BOOTSTRAP" || { echo "no SOURCE_REPO" >&2; exit 1; }
  grep -q "SOURCE_REVISION" "$BOOTSTRAP" || { echo "no SOURCE_REVISION" >&2; exit 1; }
  grep -q "ACP_PORT" "$BOOTSTRAP" || { echo "no ACP_PORT" >&2; exit 1; }
  grep -q "ACP_SECRET" "$BOOTSTRAP" || { echo "no ACP_SECRET" >&2; exit 1; }
  grep -q "WORKSPACE_DIR" "$BOOTSTRAP" || { echo "no WORKSPACE_DIR" >&2; exit 1; }
  grep -q "hydra-acp-daemon" "$BOOTSTRAP" || { echo "no hydra-acp-daemon" >&2; exit 1; }
  grep -q "auth-token" "$BOOTSTRAP" || { echo "no auth-token" >&2; exit 1; }
  grep -q "jq -n" "$BOOTSTRAP" || { echo "Hydra config is not generated with jq" >&2; exit 1; }
  grep -q "ACP_PORT must be an integer between 1 and 65535" "$BOOTSTRAP" \
    || { echo "ACP_PORT is not validated" >&2; exit 1; }

  PUBLISH="${publishPkg}/bin/publish-agent-image"
  test -f "$PUBLISH" || { echo "publish script missing" >&2; exit 1; }
  grep -q "nerdctl load" "$PUBLISH" || { echo "no nerdctl load" >&2; exit 1; }
  grep -q "nerdctl push" "$PUBLISH" || { echo "no nerdctl push" >&2; exit 1; }
  grep -q "ipfs://" "$PUBLISH" || { echo "no ipfs://" >&2; exit 1; }
  grep -q "import@sha256" "$PUBLISH" || { echo "internal nerdctl import reference is not filtered" >&2; exit 1; }
  grep -q -- '--ipfs-address' "$PUBLISH" || { echo "no explicit IPFS API address" >&2; exit 1; }
  grep -q "publication.json" "$PUBLISH" || { echo "no publication.json" >&2; exit 1; }
  if grep -q "{{.Id}}" "$PUBLISH"; then
    echo "publish script falls back to the OCI config digest" >&2
    exit 1
  fi
  if grep -q 'pull --quiet.*|| true' "$PUBLISH"; then
    echo "facade pull errors are ignored" >&2
    exit 1
  fi

  PULL="${pullPkg}/bin/pull-agent-image"
  test -f "$PULL" || { echo "pull script missing" >&2; exit 1; }
  grep -q "nerdctl pull" "$PULL" || { echo "no nerdctl pull" >&2; exit 1; }

  # Execute the source bootstrap with JSON-sensitive values. A fake daemon
  # validates the generated artifact before returning successfully.
  mkdir -p fake-bin home hydra
  cat > fake-bin/hydra-acp-daemon <<'EOF'
#!/bin/sh
set -eu
jq -e \
  --arg workspace "$EXPECTED_WORKSPACE" \
  --arg logLevel "$EXPECTED_LOG_LEVEL" \
  '.defaultCwd == $workspace and
   .agents["pi-acp"].env.PI_ACP_DIR == ($workspace + "/.pi-acp") and
   .daemon.logLevel == $logLevel and
   .daemon.port == 55514' \
  "$HYDRA_ACP_HOME/config.json" >/dev/null
touch "$BOOTSTRAP_MARKER"
EOF
  chmod +x fake-bin/hydra-acp-daemon

  export HOME="$PWD/home"
  export PATH="$PWD/fake-bin:$PATH"
  export HYDRA_ACP_HOME="$PWD/hydra"
  export EXPECTED_WORKSPACE="$PWD/workspace-\"quoted"
  export EXPECTED_LOG_LEVEL='info"quoted'
  export BOOTSTRAP_MARKER="$PWD/bootstrap-ran"
  WORKSPACE_DIR="$EXPECTED_WORKSPACE" LOG_LEVEL="$EXPECTED_LOG_LEVEL" ACP_PORT=55514 \
    bash ${../packages/agent-image/bootstrap.sh}
  test -f "$BOOTSTRAP_MARKER" || { echo "bootstrap daemon was not reached" >&2; exit 1; }

  if ACP_PORT=invalid bash ${../packages/agent-image/bootstrap.sh} >/dev/null 2>&1; then
    echo "bootstrap accepted an invalid ACP_PORT" >&2
    exit 1
  fi

  echo "agent-image contract verified" > $out
''
