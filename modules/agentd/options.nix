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
  };
}
