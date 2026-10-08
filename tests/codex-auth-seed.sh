set -euo pipefail
seed_script=$1
state=$(mktemp -d)
trap 'rm -rf "$state"' EXIT
printf '%s\n' '{"tokens":{"access_token":"fake-access","refresh_token":"fake-refresh"}}' > "$state/seed"
bash "$seed_script" "$state/seed" "$state/codex"
cmp "$state/seed" "$state/codex/auth.json"
# Token refresh must survive restarts with an unchanged encrypted seed.
printf '%s\n' '{"tokens":{"access_token":"rotated","refresh_token":"rotated"}}' > "$state/codex/auth.json"
cp "$state/codex/auth.json" "$state/rotated"
bash "$seed_script" "$state/seed" "$state/codex"
cmp "$state/rotated" "$state/codex/auth.json"
# A new seed deliberately replaces the rotated copy.
printf '%s\n' '{"tokens":{"access_token":"new-login","refresh_token":"new-refresh"}}' > "$state/seed"
bash "$seed_script" "$state/seed" "$state/codex"
cmp "$state/seed" "$state/codex/auth.json"
# Invalid input must neither replace auth nor disclose its contents.
printf '%s\n' 'invalid-credential-sentinel' > "$state/invalid"
if bash "$seed_script" "$state/invalid" "$state/codex" > "$state/log" 2>&1; then exit 1; fi
! grep -q 'invalid-credential-sentinel' "$state/log"
cmp "$state/seed" "$state/codex/auth.json"
test "$(find "$state/codex/auth.json" -perm 0600 | wc -l | tr -d ' ')" = 1
