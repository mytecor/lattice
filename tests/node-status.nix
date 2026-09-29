{ pkgs, nodeStatus }:

# Runtime smoke test for the assembled Go package and its three HTTP contracts.
# It disables systemd-unit probing because the Nix build sandbox has no PID 1.
pkgs.runCommand "node-status-runtime-test" {
  nativeBuildInputs = [ pkgs.curl pkgs.jq nodeStatus ];
} ''
  export NODE_STATUS_LISTEN_ADDRESS=127.0.0.1:19217
  export NODE_STATUS_STATE_VERSION=26.05
  export NODE_STATUS_SYSTEMD_UNITS=""
  node-status >server.log 2>&1 &
  server_pid=$!
  trap 'kill "$server_pid" 2>/dev/null || true' EXIT

  for _ in $(seq 1 50); do
    curl --fail --silent http://127.0.0.1:19217/healthz >health.json && break
    sleep 0.1
  done

  jq -e '.status == "ok"' health.json >/dev/null
  curl --fail --silent http://127.0.0.1:19217/ >status.json
  jq -e '
    .service == "lattice-node-status" and
    .stateVersion == "26.05" and
    (.system.uptimeSeconds | type == "number") and
    (.system.memoryTotalBytes | type == "number")
  ' status.json >/dev/null

  curl --fail --silent http://127.0.0.1:19217/metrics >metrics.txt
  grep -q '^node_status_up 1$' metrics.txt
  grep -q '^node_status_cpu_seconds_total{' metrics.txt
  grep -q '^node_status_memory_total_bytes ' metrics.txt
  grep -q '^node_status_filesystem_size_bytes{' metrics.txt

  mkdir "$out"
  cp health.json status.json metrics.txt "$out"/
''
