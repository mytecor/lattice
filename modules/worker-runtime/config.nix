{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.worker-runtime;
  stateDir = "/var/lib/${cfg.stateDirectory}";
  runtimeDir = "/run/${cfg.runtimeDirectory}";
  inherit (cfg) user group uid gid;

  # Identity lives in the StateDirectory by default; r1sd auto-generates the
  # key on first start and it survives reboot through the persistent state
  # directory. An explicit identityFile overrides (e.g. a seeded key).
  identity = if cfg.identityFile == null then "${stateDir}/identity" else cfg.identityFile;

  # r1sd is executed with this working directory as $HOME. All per-user r1s
  # state — the cluster credential directory and the allocator's default
  # state/log paths — lands under it and survives reboots through
  # StateDirectory/impermanence.
  #
  # Since r1s v0.5, cluster membership lives in $HOME/.config/r1s/realms/<id>`
  # (cluster.DefaultDirectory() resolves via os.UserHomeDir), so the service
  # MUST export HOME=<homeDir>. systemd system users default to HOME=/var/empty
  # (read-only, non-persistent), which would make `r1sd cluster join` and the
  # daemon's cluster.Resolve disagree on where the credential lives.
  homeDir = stateDir;

  # Resolved cluster selector (full public ID) written by preStart; the
  # ExecStart wrapper reads it positionally. Absence triggers a fresh join.
  clusterStateFile = "${homeDir}/cluster-id";

  # r1sd wrapper: systemd ExecStart does NOT run through a shell, so an inline
  # '$(cat …/cluster-id)' in ExecStart would be passed as a literal (and worse,
  # systemd treats '$(cat' as an env-var reference and refuses to start). The
  # cluster id is only known at runtime (after preStart joins), so we resolve it
  # in a real shell wrapper that reads the state file and execs r1sd.
  r1sdWrapper = pkgs.writeShellScript "r1sd-worker" ''
    set -eu
    if [ ! -r ${lib.escapeShellArg clusterStateFile} ]; then
      echo "lattice.worker-runtime: cluster-id state file missing" >&2
      echo "  (${lib.escapeShellArg clusterStateFile})" >&2
      echo "  preStart (r1sd cluster join) must run before the daemon." >&2
      exit 1
    fi
    exec ${lib.getExe' cfg.package "r1sd"} ${lib.escapeShellArgs r1sdArgs} "$(cat ${lib.escapeShellArg clusterStateFile})"
  '';

  # r1s/r1sd attach as clients to an already-running shared RNS
  # instance (the node's rns-server with share_instance = Yes) and never build
  # a private Reticulum stack. There is no --rns-config anymore. Cluster
  # membership is a per-user credential stored by `r1sd cluster join` under
  # $HOME/.config/r1s/realms/<cluster-id>`; the running daemon selects the
  # cluster positionally by ID. This module therefore wires:
  #
  #   preStart  r1sd cluster join <token>   (once, from the agenix secret)
  #   ExecStart r1sd [allocator flags] <cluster-id>
  #
  # The cluster ID is a short public digest (not secret). Instead of parsing
  # `cluster join` output, preStart derives it with `r1sd cluster list` and
  # stores it in a state file the ExecStart wrapper reads.

  # --- argv for the r1sd daemon (no --rns-config; cluster ID is positional).
  r1sdArgs = [
    "--identity" identity
    "--capacity" cfg.capacity
    "--containerd-address" cfg.containerdAddress
    "--containerd-namespace" cfg.containerdNamespace
    "--announce-interval" cfg.announceInterval
  ] ++ lib.optionals (cfg.containerdSnapshotter != "")
    [ "--containerd-snapshotter" cfg.containerdSnapshotter ]
  ++ lib.optionals (cfg.node != null)
    [ "--node" cfg.node ]
  ++ lib.optionals cfg.tunnelEnabled
    [ "--tunnel-enabled" ];

  # Allocator service dependencies: the shared RNS instance and containerd.
  # F22 r1s/r1sd attach as clients to an already-running shared RNS instance
  # and fail closed when it is absent (ErrSharedInstanceUnavailable) — they
  # never build their own Reticulum stack and can never become the server. The
  # worker must therefore come up only after whatever unit hosts that shared
  # instance (rnsInstanceService, default rns-server) and after containerd.
  #
  # rnsInstanceService is a bare unit name; systemd `after`/`wants` need a
  # qualified unit, so normalize to the .service form.
  rnsUnit = if lib.hasSuffix ".service" cfg.rnsInstanceService
    then cfg.rnsInstanceService
    else "${cfg.rnsInstanceService}.service";
  backendAfter = [ "network-online.target" "containerd.service" rnsUnit ];
  backendWants = [ "network-online.target" "containerd.service" rnsUnit ];

  # preStart: join the cluster when needed and persist the resolved cluster ID.
  # The token is a sensitive agenix secret read only at runtime (never baked
  # into the store or ExecStart argv); `r1sd cluster join` prints only the
  # Cluster ID and credential path (no token) to the journal. `cluster list`
  # prints the same public IDs, which is what the ExecStart wrapper consumes.
  joinScript = ''
    set -eu
    ${pkgs.coreutils}/bin/install -d -m 0700 \
      -o ${lib.escapeShellArg user} -g ${lib.escapeShellArg group} \
      ${lib.escapeShellArg homeDir}

    # v0.5 changed the authentication domain and deliberately does not import
    # the v0.4 clusters store. Rejoin when the persisted selector is absent or
    # no longer names a credential returned by the installed r1sd.
    ${lib.getExe' cfg.package "r1sd"} cluster list > "${homeDir}/cluster-list.tmp"
    if [ ! -s "${homeDir}/cluster-list.tmp" ] \
      || [ ! -r ${lib.escapeShellArg clusterStateFile} ] \
      || ! ${pkgs.gnugrep}/bin/grep -Fxq \
        "$(${pkgs.coreutils}/bin/tr -d '[:space:]' < ${lib.escapeShellArg clusterStateFile})" \
        "${homeDir}/cluster-list.tmp"; then
      # token file must be readable by the service user
      ${pkgs.gnused}/bin/sed -n '1p' ${lib.escapeShellArg cfg.clusterTokenFile} \
        > "${homeDir}/join-token.tmp"
      ${pkgs.coreutils}/bin/chmod 0600 "${homeDir}/join-token.tmp"
      ${pkgs.coreutils}/bin/chown ${lib.escapeShellArg "${user}:${group}"} \
        "${homeDir}/join-token.tmp"
      TOKEN=$(${pkgs.coreutils}/bin/cat "${homeDir}/join-token.tmp")
      ${lib.getExe' cfg.package "r1sd"} cluster join "$TOKEN"
      ${pkgs.coreutils}/bin/rm -f "${homeDir}/join-token.tmp"
      ${lib.getExe' cfg.package "r1sd"} cluster list > "${homeDir}/cluster-id.tmp"
    else
      ${pkgs.coreutils}/bin/cp "${homeDir}/cluster-list.tmp" "${homeDir}/cluster-id.tmp"
    fi
    ${pkgs.coreutils}/bin/rm -f "${homeDir}/cluster-list.tmp"

    # This node is configured for exactly one execution cluster. Refuse an
    # ambiguous credential store instead of concatenating multiple IDs.
    test "$(${pkgs.coreutils}/bin/wc -l < "${homeDir}/cluster-id.tmp")" -eq 1
    ${pkgs.coreutils}/bin/tr -d '[:space:]' < "${homeDir}/cluster-id.tmp" \
      > "${homeDir}/cluster-id"
    ${pkgs.coreutils}/bin/rm -f "${homeDir}/cluster-id.tmp"
  '';
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.clusterTokenFile != null;
        message = ''
          lattice.worker-runtime requires clusterTokenFile (path to an agenix
          secret holding the 'r1s1:<...>' join token). r1sd refuses to start
          without cluster membership; the token is consumed once by preStart.
        '';
      }
    ];

    # nerdctl on the host PATH for OCI image management / IPFS registry facade CLI.
    environment.systemPackages = [ cfg.package pkgs.nerdctl ];

    # Enable containerd and expose its gRPC socket to the r1s group only
    # (0660, group = r1s) — r1sd is the sole local consumer, nothing opens the
    # socket to the network. The socket stays at the default
    # /run/containerd/containerd.sock.
    virtualisation.containerd = {
      enable = true;
      settings = {
        version = 2;
        grpc = {
          # Ownership of the unix socket; containerd creates it 0660 by
          # default, so group-write grants the r1s user access. uid kept at 0
          # (root) so containerd's own tools keep working. gid is a fixed
          # literal number (see lattice.worker-runtime.gid) — auto-assigned
          # ids are null at TOML-render time and cannot be referenced here.
          gid = gid;
        };
      };
    };

    users.groups.${group}.gid = gid;

    users.users.${user} = {
      uid = uid;
      isSystemUser = true;
      group = group;
      home = homeDir;
      createHome = true;
      # r1sd needs to pull OCI images and talk to containerd; the gRPC socket
      # is root:r1s 0660 (see containerd settings above), so no extra groups
      # are required.
    };

    systemd.services.worker-runtime = {
      description = "r1sd allocator (F10 disposable-worker execution backend)";
      wantedBy = [ "multi-user.target" ];
      after = backendAfter;
      wants = backendWants;

      path = [ cfg.package ];

      preStart = joinScript;

      serviceConfig = {
        User = user;
        Group = group;
        StateDirectory = cfg.stateDirectory;
        StateDirectoryMode = "0700";
        StateDirectoryPreserve = "restart";
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        RuntimeDirectoryPreserve = "restart";
        WorkingDirectory = homeDir;
        # HOME must point at the StateDirectory (not /var/empty): F22 cluster
        # credentials resolve through os.UserHomeDir() to
        # $HOME/.config/r1s/realms/<id>, and the allocator's default state /
        # log paths land beside the identity file under the same directory.
        Environment = [ "HOME=${homeDir}" ];
        # ExecStart is a shell wrapper (systemd does not expand $(...) inside
        # ExecStart argv): the wrapper reads the cluster-id resolved by preStart
        # and execs r1sd with the allocator flags. Never inline a command
        # substitution directly into ExecStart.
        #
        # writeShellScript outputs the script itself at $out (a single file,
        # no bin/ subdir), so ExecStart is the wrapper derivation directly —
        # appending '/bin/r1sd-worker' would 203/EXEC.
        ExecStart = r1sdWrapper;
        Restart = "on-failure";
        RestartSec = 5;
        UMask = "0077";

        # Strict by default. Execution tunnels additionally enter the task's
        # network namespace and bring up its loopback interface, which upstream
        # r1s explicitly requires CAP_SYS_ADMIN + CAP_NET_ADMIN for.
        AmbientCapabilities = lib.optionals cfg.tunnelEnabled [ "CAP_SYS_ADMIN" "CAP_NET_ADMIN" ];
        CapabilityBoundingSet = lib.optionals cfg.tunnelEnabled [ "CAP_SYS_ADMIN" "CAP_NET_ADMIN" ];
        LockPersonality = true;
        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        # Tunnel setup resolves /proc/<container-pid>/ns/net. Hide other
        # processes when tunnels are disabled, expose the namespace path only
        # for the explicitly enabled tunnel runtime.
        ProtectProc = if cfg.tunnelEnabled then "default" else "invisible";
        ProtectSystem = "full";
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
      };
    };
  };
}
