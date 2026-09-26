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
# r1sd runs from the pinned lattice package.
assert lib.hasPrefix (lib.getExe' pkgs.lattice.r1s "r1sd") execStart;
# F22 cluster credentials resolve via os.UserHomeDir() to
# $HOME/.config/r1s/clusters/<id>; the unit must export HOME pointing at the
# persistent StateDirectory (systemd system users otherwise default to
# /var/empty, which is read-only and non-persistent).
assert builtins.elem "HOME=/var/lib/worker-runtime" unit.serviceConfig.Environment;
# F22 exec shape: allocator flags then a positional cluster selector (resolved
# by preStart into the state file). No --rns-config, no --cluster flag.
assert lib.hasInfix "--identity" execStart;
assert lib.hasInfix "${cfg.stateDirectory}" execStart;
assert lib.hasInfix "--capacity default=1" execStart;
assert lib.hasInfix "--containerd-address /run/containerd/containerd.sock" execStart;
assert lib.hasInfix "--containerd-namespace r1s" execStart;
assert lib.hasInfix "--announce-interval 5m" execStart;
assert lib.hasInfix "--containerd-snapshotter overlayfs" execStart;
assert lib.hasInfix "--node" execStart;
assert lib.hasInfix "region" execStart;
# Cluster is positional: the command tail is r1sd ... <cluster-id>. The ID is
# read at runtime from the state file (never baked into ExecStart); preStart is
# responsible for joining and resolving it.
assert lib.hasInfix "\$(cat /var/lib/worker-runtime/cluster-id)" execStart;
assert !lib.hasInfix "--rns-config" execStart;
assert !lib.hasInfix "--cluster" execStart;
# containerd is enabled and its gRPC socket is opened to the r1s group only.
assert config.virtualisation.containerd.enable;
assert containerdSettings.grpc.gid == cfg.gid;
assert config.users.groups.r1s.gid == cfg.gid;
assert config.users.users.r1s.uid == cfg.uid;
assert !(builtins.hasAttr "address" containerdSettings.grpc);
# Strict sandbox: no capabilities, no netlink, no listening socket required.
assert unit.serviceConfig.AmbientCapabilities == "";
assert unit.serviceConfig.CapabilityBoundingSet == "";
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
pkgs.runCommand "worker-runtime-evaluation" { } ''
  # ExecStart must carry the identity path (auto-generated default) and the
  # full command line, but never the join token value.
  ${pkgs.coreutils}/bin/grep -q -- '--identity /var/lib/worker-runtime/identity' \
    <<< "${execStart}"
  if grep -q 'placeholder-not-a-real-token' <<< "${execStart}"; then
    echo "join token leaked into ExecStart" >&2
    exit 1
  fi

  # A resolved cluster ID must not be baked into the unit (it is only known at
  # runtime after preStart joins); ExecStart reads it positionally from the
  # state file.
  if grep -q 'r1s[0-9a-f]\{16\}' <<< "${execStart}"; then
    echo "baked cluster identifier leaked into ExecStart" >&2
    exit 1
  fi
  touch $out
''
