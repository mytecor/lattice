{ lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.worker-runtime = {
    enable = mkEnableOption "r1sd allocator (F10 disposable-worker execution backend)";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.r1s;
      defaultText = lib.literalExpression "pkgs.lattice.r1s";
      description = "Pinned r1s package providing the r1s client and r1sd allocator.";
    };

    user = mkOption {
      type = types.str;
      default = "r1s";
      description = "Dedicated system user that runs r1sd.";
    };

    group = mkOption {
      type = types.str;
      default = "r1s";
      description = "Dedicated system group that owns r1sd identity/state.";
    };

    uid = mkOption {
      type = types.ints.between 1 999;
      default = 634;
      description = ''
        System uid for the r1s user. Explicit and fixed (not auto-assigned):
        containerd's gRPC socket ownership is configured with this number in
        its TOML, which must be a literal at eval time (auto-assigned ids are
        null/unknown in the merged option). Change only if 634 is taken on a
        node; keep user/group/uid/gid in lockstep.
      '';
    };

    gid = mkOption {
      type = types.ints.between 1 999;
      default = 634;
      description = ''
        System gid for the r1s group. Like uid, fixed because containerd's
        socket group is a literal number in its TOML. Kept equal to uid by
        default so the service owns its state outright.
      '';
    };

    stateDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "worker-runtime";
      description = "systemd StateDirectory name below /var/lib.";
    };

    runtimeDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "worker-runtime";
      description = "systemd RuntimeDirectory name below /run.";
    };

    # --- allocator resource / placement -----------------------------------
    capacity = mkOption {
      type = types.str;
      default = "default=1";
      description = "Comma-separated r1sd resource capacities, e.g. default=2,gpu=1.";
    };

    node = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Optional JSON node capabilities advertisement for placement, e.g.
        '{"labels":{"region":"eu"}}'. Passed to r1sd --node as-is; os/arch
        default to the build target.
      '';
    };

    announceInterval = mkOption {
      type = types.str;
      default = "5m";
      description = "Service announce refresh interval (r1sd --announce-interval).";
    };

    # --- containerd ----------------------------------------------------------
    containerdAddress = mkOption {
      type = types.str;
      default = "/run/containerd/containerd.sock";
      description = "Path to the containerd Unix socket r1sd connects to.";
    };

    containerdNamespace = mkOption {
      type = types.str;
      default = "r1s";
      description = "containerd namespace isolating r1s workload metadata.";
    };

    containerdSnapshotter = mkOption {
      type = types.str;
      default = "";
      description = "containerd snapshotter override (daemon default when empty).";
    };

    # --- shared RNS instance (F22) -----------------------------------------
    #
    # F22 cutover: r1s/r1sd attach as clients to an already-running shared RNS
    # instance and never build a private Reticulum stack (there is no
    # --rns-config anymore). This is unconditional — the binaries fail closed
    # with "RNS shared instance is not running" when no shared instance is
    # reachable and can never become the server themselves. So there is no
    # toggle here, only the unit ordering below.

    rnsInstanceService = mkOption {
      type = types.str;
      default = "rns-server";
      description = ''
        systemd unit that hosts the shared RNS instance the worker attaches to.
        The worker service is ordered after it (and after containerd). Point it
        at any service providing the platform-default shared instance socket
        (@rns/default or TCP 37428), e.g. a dedicated Reticulum-Go daemon.
      '';
    };

    # --- identity / cluster membership ------------------------------------------
    identityFile = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Path to the persistent private RNS identity file for the allocator.
        When null, r1sd generates a fresh identity on first start inside the
        StateDirectory ('/var/lib/<stateDirectory>/identity') — no operator
        secret is needed and the key survives reboot through the persistent
        state directory. Set an explicit path to seed a known identity.
      '';
    };

    clusterTokenFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Path to a file containing the r1s cluster join token ('r1s1:<...>')
        the allocator joins with on first start. Sensitive — supply an agenix
        secret path (0600, readable by the r1s user). The token is read at
        runtime in preStart and is never baked into the Nix store or the
        ExecStart argv. Required when enable = true: r1sd refuses to start
        without cluster membership.
      '';
    };
  };
}
