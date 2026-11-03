# Regression test for the radicle-node 100% CPU busy-loop caused by gossiped
# bracketed IPv6 literals stored with `type = 'dns'` in `node/node.db`:
# 1. Both `radicle-node.service` (seed profile) and `radicle-peer-node.service`
#    (node-dev profile) wire `lattice-radicle-db-guard` into `ExecStartPre` and
#    `ExecStartPost` (`+` root-namespace prefix on the chroot-confined seed unit).
# 2. `lattice-radicle-db-guard` normalizes pre-existing malformed `type='dns'`
#    bracketed IPv6 rows to `type='ipv6'` and installs the `fix_dns_ipv6_type`
#    trigger so future inserts and duplicate re-announcements never leave a
#    `type='dns'` row with `last_attempt IS NULL`.

{ lib, pkgs, radicleUnit, radiclePeerUnit }:

let
  guardExe = lib.getExe pkgs.lattice.radicle-db-guard;
  hasGuard = prefix: list:
    builtins.any (cmd: lib.hasPrefix "${prefix}${guardExe} " cmd) list;
in
assert hasGuard "+" (radicleUnit.serviceConfig.ExecStartPre or [ ]);
assert hasGuard "+" (radicleUnit.serviceConfig.ExecStartPost or [ ]);
assert hasGuard "" (radiclePeerUnit.serviceConfig.ExecStartPre or [ ]);
assert hasGuard "" (radiclePeerUnit.serviceConfig.ExecStartPost or [ ]);

pkgs.runCommand "radicle-db-guard-test" {
  nativeBuildInputs = [ pkgs.sqlite pkgs.lattice.radicle-db-guard ];
} ''
  set -euo pipefail

  # 1. Uninitialized RAD_HOME (no node.db yet) must exit 0 cleanly.
  empty_home="$TMPDIR/empty"
  mkdir -p "$empty_home"
  lattice-radicle-db-guard "$empty_home"

  # 2. Populate a node.db with radicle-node's strict schema and a poisoned
  #    type='dns' bracketed IPv6 row.
  rad_home="$TMPDIR/rad"
  mkdir -p "$rad_home/node"
  db="$rad_home/node/node.db"

  sqlite3 "$db" <<'SQL'
  CREATE TABLE "nodes" (
    "id" text primary key not null,
    "features" integer not null,
    "alias" text not null,
    "pow" integer default 0,
    "timestamp" integer not null,
    "banned" integer default false,
    "penalty" integer not null default 0,
    "version" integer not null default 1,
    "agent" text not null default "/radicle/"
  ) strict;
  CREATE TABLE "addresses" (
    "node" text not null references "nodes" ("id") on delete cascade,
    "type" text not null,
    "value" text not null,
    "source" text not null,
    "timestamp" integer not null,
    "last_attempt" integer default null,
    "last_success" integer default null,
    "banned" integer default false,
    unique ("node", "type", "value")
  ) strict;

  INSERT INTO nodes VALUES ('z6MknSER62Zrmn18YMXBRJY13wW2nqjUAMutBnFPLth58Qsr', 1, 'bad.seed', 0, 1000, 0, 0, 1, '/radicle:1/');
  INSERT INTO addresses VALUES ('z6MknSER62Zrmn18YMXBRJY13wW2nqjUAMutBnFPLth58Qsr', 'dns', '[2a02:8108:2781:ac00:2ecf:67ff:fec6:d2f6]:8776', 'peer', 1000, NULL, NULL, 0);
  INSERT INTO addresses VALUES ('z6MknSER62Zrmn18YMXBRJY13wW2nqjUAMutBnFPLth58Qsr', 'dns', 'iris.radicle.network:8776', 'peer', 1000, NULL, NULL, 0);
  SQL

  lattice-radicle-db-guard "$rad_home"

  # Pre-existing bracketed IPv6 row must be normalized to type='ipv6'; legitimate
  # DNS hostname must remain type='dns'.
  test "$(sqlite3 "$db" "SELECT type FROM addresses WHERE value = '[2a02:8108:2781:ac00:2ecf:67ff:fec6:d2f6]:8776';")" = "ipv6"
  test "$(sqlite3 "$db" "SELECT type FROM addresses WHERE value = 'iris.radicle.network:8776';")" = "dns"

  # Simulate radicle-node recording last_attempt using parsed AddressType::Ipv6.
  sqlite3 "$db" "UPDATE addresses SET last_attempt = 12345 WHERE node = 'z6MknSER62Zrmn18YMXBRJY13wW2nqjUAMutBnFPLth58Qsr' AND type = 'ipv6' AND value = '[2a02:8108:2781:ac00:2ecf:67ff:fec6:d2f6]:8776';"
  test "$(sqlite3 "$db" "SELECT last_attempt FROM addresses WHERE value = '[2a02:8108:2781:ac00:2ecf:67ff:fec6:d2f6]:8776';")" = "12345"

  # 3. A repeat gossip insert of the same malformed (node, 'dns', '[2a02:...]:8776')
  #    must not fail the UNIQUE constraint and must preserve the ipv6 row's last_attempt.
  sqlite3 "$db" "INSERT INTO addresses VALUES ('z6MknSER62Zrmn18YMXBRJY13wW2nqjUAMutBnFPLth58Qsr', 'dns', '[2a02:8108:2781:ac00:2ecf:67ff:fec6:d2f6]:8776', 'peer', 2000, NULL, NULL, 0);"
  test "$(sqlite3 "$db" "SELECT count(*) FROM addresses WHERE type = 'dns' AND value LIKE '[%';")" = "0"
  test "$(sqlite3 "$db" "SELECT last_attempt FROM addresses WHERE type = 'ipv6' AND value = '[2a02:8108:2781:ac00:2ecf:67ff:fec6:d2f6]:8776';")" = "12345"

  # 4. A brand-new malformed gossip insert for another IPv6 address must be
  #    rewritten to type='ipv6' on insert by the trigger.
  sqlite3 "$db" "INSERT INTO addresses VALUES ('z6MknSER62Zrmn18YMXBRJY13wW2nqjUAMutBnFPLth58Qsr', 'dns', '[2001:db8::1]:8776', 'peer', 3000, NULL, NULL, 0);"
  test "$(sqlite3 "$db" "SELECT type FROM addresses WHERE value = '[2001:db8::1]:8776';")" = "ipv6"

  touch "$out"
''
