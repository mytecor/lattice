# Invoked by writeShellApplication before the gateway starts, with no agents alive.
set -euo pipefail
umask 077
source_file=$1
codex_dir=$2
# Parse without ever printing credentials, including on malformed input.
if ! jq -e 'type == "object" and
  (.tokens | type == "object") and
  (.tokens.access_token | type == "string" and length > 0) and
  (.tokens.refresh_token | type == "string" and length > 0)' \
  "$source_file" >/dev/null 2>&1; then
  echo 'codex: credential must be a ChatGPT auth.json with access and refresh tokens' >&2
  exit 1
fi
mkdir -p "$codex_dir"
chmod 0700 "$codex_dir"
seed_digest=$(sha256sum "$source_file" | cut -d ' ' -f 1)
previous_digest=""
if [ -f "$codex_dir/.auth-seed-sha256" ]; then
  previous_digest=$(< "$codex_dir/.auth-seed-sha256")
fi
if [ ! -f "$codex_dir/auth.json" ] || [ "$seed_digest" != "$previous_digest" ]; then
  install -m 0600 "$source_file" "$codex_dir/.auth.json.new"
  mv -f "$codex_dir/.auth.json.new" "$codex_dir/auth.json"
  printf '%s\n' "$seed_digest" > "$codex_dir/.auth-seed-sha256.new"
  mv -f "$codex_dir/.auth-seed-sha256.new" "$codex_dir/.auth-seed-sha256"
fi
