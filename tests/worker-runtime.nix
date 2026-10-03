{ nixpkgs, pkgs, workerRuntimeModule }:

let
  inherit (nixpkgs) lib;

  # A placeholder token *value* never appears here — the file content is only
  # read at runtime by r1sd cluster join. The test only wires the option and
  # checks the generated unit/config, it never runs r1sd.
  tokenFile = pkgs.writeText "cluster-token" "r1s1:placeholder-not-a-real-token";

  config = (lib.nixosSystem {
    modules = [
      workerRuntimeModule
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";

        lattice.worker-runtime = {
          enable = true;
          clusterTokenFile = tokenFile;
          tunnelEnabled = true;
          # Exercise the optional flag path (flat argv) so a nested-list
          # regression is caught at eval time.
          node = ''{"labels":{"region":"eu"}}'';
          containerdSnapshotter = "overlayfs";
          # F22: the worker attaches to the node's shared RNS instance. The
          # test module does not import rns-server, so keep the shared-instance
          # requirement asserted against the rns-server option but not force
          # one here (the assertion tolerates an absent rns-server module).
          rnsInstanceService = "rns-server";
        };
      }
    ];
  }).config;

  cfg = config.lattice.worker-runtime;
  unit = config.systemd.services.worker-runtime;
  execStart = unit.serviceConfig.ExecStart;
  containerdSettings = config.virtualisation.containerd.settings;
in
assert cfg.enable;
assert cfg.clusterTokenFile == tokenFile;
# Service is a foreground systemd service started at multi-user, after
# network/containerd and the shared RNS instance.
assert lib.elem "multi-user.target" unit.wantedBy;
assert lib.elem "containerd.service" unit.after;
assert lib.elem "rns-server.service" unit.after;
assert lib.elem "network-online.target" unit.after;
assert lib.elem "containerd.service" unit.wants;
assert lib.elem "rns-server.service" unit.wants;
# ExecStart must NOT be a raw r1sd invocation with inline $(cat …) — systemd
# does not expand command substitutions inside ExecStart argv (it treats
# '$(cat' as an env-var reference and fails). The r1sd flags + runtime
# cluster-id resolution live in a shell wrapper (r1sd-worker) instead.
# writeShellScript emits the script at $out (no bin/ subdir), so ExecStart is
# the wrapper derivation path itself, ending in '-r1sd-worker'.
assert lib.hasSuffix "-r1sd-worker" execStart;
# F22 cluster credentials resolve via os.UserHomeDir() to
# $HOME/.config/r1s/clusters/<id>; the unit must export HOME pointing at the
# persistent StateDirectory (systemd system users otherwise default to
# /var/empty, which is read-only and non-persistent).
assert builtins.elem "HOME=/var/lib/worker-runtime" unit.serviceConfig.Environment;
# containerd is enabled and its gRPC socket is opened to the r1s group only.
assert config.virtualisation.containerd.enable;
assert containerdSettings.grpc.gid == cfg.gid;
assert config.users.groups.r1s.gid == cfg.gid;
assert config.users.users.r1s.uid == cfg.uid;
assert !(builtins.hasAttr "address" containerdSettings.grpc);
# Tunnel mode gets only the two capabilities required to enter the task netns
# and initialize loopback; it must also be able to resolve /proc/<pid>/ns/net.
assert lib.all (capability: lib.elem capability unit.serviceConfig.AmbientCapabilities) [ "CAP_SYS_ADMIN" "CAP_NET_ADMIN" ];
assert lib.all (capability: lib.elem capability unit.serviceConfig.CapabilityBoundingSet) [ "CAP_SYS_ADMIN" "CAP_NET_ADMIN" ];
assert unit.serviceConfig.ProtectProc == "default";
assert unit.serviceConfig.NoNewPrivileges == true;
assert lib.elem "AF_UNIX" unit.serviceConfig.RestrictAddressFamilies;
assert lib.elem "AF_INET" unit.serviceConfig.RestrictAddressFamilies;
assert lib.elem "AF_INET6" unit.serviceConfig.RestrictAddressFamilies;
assert !(lib.elem "AF_NETLINK" unit.serviceConfig.RestrictAddressFamilies);
# preStart is present (cluster join bootstrap + cluster-id resolution) and
# touches the cluster-id state file.
assert builtins.isString unit.preStart;
assert lib.hasInfix "cluster join" unit.preStart;
assert lib.hasInfix "cluster-id" unit.preStart;
# The r1sd argv lives inside the wrapper script (writeShellScript), not in
# the unit's ExecStart. We inspect the wrapper text here for the F22 flag
# shape and the no-secret/no-baked-cluster-id guarantees so the unit stays
# declarative even though the flags moved out of ExecStart.
pkgs.runCommand "worker-runtime-evaluation" { } ''
  WRAPPER=${lib.escapeShellArg execStart}
  test -f "$WRAPPER" || { echo "wrapper missing: $WRAPPER" >&2; exit 1; }
  # Wrapper body (the r1sd argv that systemd cannot inline-expand) must run
  # the pinned r1sd with the F22 flag shape — no --rns-config and no --cluster
  # (shared-instance contract; cluster id is a positional runtime selector).
  grep -q -- '${lib.getExe' pkgs.lattice.r1s "r1sd"}' "$WRAPPER" \
    || { echo 'pinned r1sd not execed in wrapper' >&2; exit 1; }
  grep -q -- '--identity' "$WRAPPER" || { echo 'no --identity' >&2; exit 1; }
  grep -q -- '${cfg.stateDirectory}' "$WRAPPER" \
    || { echo 'no stateDirectory' >&2; exit 1; }
  grep -q -- '--capacity default=1' "$WRAPPER" || { echo 'no --capacity' >&2; exit 1; }
  grep -q -- '--containerd-address /run/containerd/containerd.sock' "$WRAPPER" \
    || { echo 'no --containerd-address' >&2; exit 1; }
  grep -q -- '--containerd-namespace r1s' "$WRAPPER" \
    || { echo 'no --containerd-namespace' >&2; exit 1; }
  grep -q -- '--tunnel-enabled' "$WRAPPER" \
    || { echo 'no --tunnel-enabled' >&2; exit 1; }
  grep -q -- '--announce-interval 5m' "$WRAPPER" \
    || { echo 'no --announce-interval' >&2; exit 1; }
  grep -q -- '--containerd-snapshotter overlayfs' "$WRAPPER" \
    || { echo 'no --containerd-snapshotter' >&2; exit 1; }
  grep -q -- '--node' "$WRAPPER" || { echo 'no --node' >&2; exit 1; }
  grep -q -- 'region' "$WRAPPER" || { echo 'no region' >&2; exit 1; }
  # Cluster id is read at runtime from the state file, never baked in.
  grep -q -- '$(cat /var/lib/worker-runtime/cluster-id)' "$WRAPPER" \
    || { echo 'wrapper does not read cluster-id at runtime' >&2; exit 1; }
  if grep -q -- '--rns-config' "$WRAPPER"; then
    echo '--rns-config leaked into wrapper' >&2; exit 1
  fi
  if grep -q -- '--cluster' "$WRAPPER"; then
    echo '--cluster flag leaked into wrapper' >&2; exit 1
  fi
  # No join token and no resolved cluster id may be baked in.
  if grep -q 'placeholder-not-a-real-token' "$WRAPPER"; then
    echo 'join token leaked into wrapper' >&2; exit 1
  fi
  if grep -q 'r1s[0-9a-f]\{16\}' "$WRAPPER"; then
    echo 'baked cluster identifier leaked into wrapper' >&2; exit 1
  fi
  touch $out
''
