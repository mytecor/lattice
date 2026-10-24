{ lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.agentd = {
    enable = mkEnableOption "agentd, the Lattice agent execution daemon";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.agentd;
      defaultText = lib.literalExpression "pkgs.lattice.agentd";
      description = "The agentd package to run.";
    };

    user = mkOption {
      type = types.str;
      default = "agentd";
      description = "System user running the daemon.";
    };

    group = mkOption {
      type = types.str;
      default = "agentd";
      description = "System group of the daemon user.";
    };

    realmKeyFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to file containing 32-byte realm key (e.g. agenix secret).";
    };

    identityFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = "Path to RNS identity file. If null, stored in StateDirectory.";
    };

    stateDirectory = mkOption {
      type = types.str;
      default = "lattice-agentd";
      description = "Directory under /var/lib for persistent identity and state.";
    };

    agentSubject = mkOption {
      type = types.str;
      default = "agent.run";
      description = "Meshbus subject for agent.run requests.";
    };

    workDir = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Directory for temporary worktrees during workload execution.";
    };

    runtime = mkOption {
      type = types.enum [ "dummy" "r1s" ];
      default = "dummy";
      description = "Execution backend. r1s uses disposable ACP agents; full remote verification follows in f19-03.";
    };

    r1s = {
      clusterKeyFile = mkOption {
        type = types.nullOr types.path;
        default = null;
        description = "Runtime file containing the r1s cluster membership key (32 raw bytes or 64 hex characters), separate from the Meshbus realm key.";
      };
      bootstrapDestinations = mkOption {
        type = types.listOf types.str;
        default = [ ];
        description = "Public allocator destination hashes used for deterministic bootstrap.";
      };
      tunnelPeers = mkOption {
        type = types.listOf types.str;
        default = [ ];
        description = "Bootstrap peer URIs for the run-owned tunnel edge; empty uses r1s defaults.";
      };
      resourceClass = mkOption {
        type = types.str;
        default = "default";
        description = "Allocator resource class requested for the agent workload.";
      };
      acpPort = mkOption {
        type = types.port;
        default = 55514;
        description = "ACP port inside the container, accessible only through the r1s tunnel.";
      };
      offerWait = mkOption {
        type = types.ints.positive;
        default = 10;
        description = "Offer collection window in seconds.";
      };
      agentEnvironmentFile = mkOption {
        type = types.nullOr types.path;
        default = null;
        description = "Runtime JSON string map with scoped agent environment. Passed only to workloads, never read into the Nix store.";
      };
    };

    maxIterations = mkOption {
      type = types.int;
      default = 5;
      description = "Default maximum iterations for agent execution loop.";
    };
  };
}
