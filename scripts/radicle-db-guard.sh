#!/usr/bin/env bash
# Sanitize radicle-node's SQLite address book (`node/node.db`) against malformed
# peer announcements where a bracketed IPv6 literal (`[2a02:...]:8776`) is
# gossiped with wire type `dns` instead of `ipv6`.
#
# Upstream bug (heartwood / radicle-node 1.10.x):
# 1. Gossip inserts `(node, type='dns', value='[2a02:...]:8776')` into `addresses`.
# 2. On connection maintenance, `radicle-node` selects peers with
#    `last_attempt IS NULL` and parses `value` via `FromStr`, which classifies
#    `[...]:port` as `AddressType::Ipv6` (`type='ipv6'`).
# 3. After the connection attempt fails (e.g. `Network is unreachable (os error 101)`),
#    `UPDATE addresses SET last_attempt = ? WHERE node = ? AND type = 'ipv6' AND value = ?`
#    matches 0 rows because the stored `type` is `'dns'`.
# 4. `last_attempt` stays `NULL` forever, causing `radicle-node` to retry the
#    same address in an unthrottled 100% CPU busy-loop.
#
# This guard normalizes any existing `type='dns'` bracketed IPv6 rows to
# `type='ipv6'` and installs a persistent SQLite `AFTER INSERT` trigger so
# future malformed gossip announcements are normalized on insert.

set -euo pipefail

wait_for_db=0
if [ "${1:-}" = "--wait" ]; then
  wait_for_db=1
  shift
fi

rad_home="${1:-${RAD_HOME:-}}"
if [ -z "$rad_home" ]; then
  echo "radicle-db-guard: RAD_HOME argument or environment variable is required" >&2
  exit 1
fi

db="$rad_home/node/node.db"

has_addresses_table() {
  [ -f "$db" ] || return 1
  local found
  found=$(sqlite3 -cmd ".timeout 2000" "$db" "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'addresses';" 2>/dev/null || true)
  [ "$found" = "1" ]
}

if [ "$wait_for_db" -eq 1 ]; then
  attempts=60
  while ! has_addresses_table; do
    attempts=$((attempts - 1))
    if [ "$attempts" -le 0 ]; then
      # If the peer profile has not been initialized yet (`rad-peer auth`), do
      # not fail service startup.
      exit 0
    fi
    sleep 0.5
  done
else
  if ! has_addresses_table; then
    exit 0
  fi
fi

sqlite3 "$db" <<'SQL' >/dev/null
PRAGMA busy_timeout = 5000;
BEGIN IMMEDIATE;
DELETE FROM addresses
WHERE type = 'dns' AND value LIKE '[%'
  AND EXISTS (
    SELECT 1 FROM addresses AS existing
    WHERE existing.node = addresses.node
      AND existing.type = 'ipv6'
      AND existing.value = addresses.value
  );
UPDATE addresses SET type = 'ipv6' WHERE type = 'dns' AND value LIKE '[%';
DROP TRIGGER IF EXISTS fix_dns_ipv6_type;
CREATE TRIGGER fix_dns_ipv6_type
AFTER INSERT ON addresses
WHEN NEW.type = 'dns' AND NEW.value LIKE '[%'
BEGIN
  DELETE FROM addresses
  WHERE rowid = NEW.rowid
    AND EXISTS (
      SELECT 1 FROM addresses
      WHERE node = NEW.node AND type = 'ipv6' AND value = NEW.value
    );
  UPDATE addresses
  SET type = 'ipv6'
  WHERE rowid = NEW.rowid;
END;
COMMIT;
SQL
