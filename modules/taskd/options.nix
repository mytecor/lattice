{ lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.taskd = {
    enable = mkEnableOption "taskd, the Git task reconciler daemon";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.taskd;
      defaultText = lib.literalExpression "pkgs.lattice.taskd";
      description = "The taskd package to run.";
    };

    user = mkOption {
      type = types.str;
      default = "taskd";
      description = "System user running the daemon.";
    };

    group = mkOption {
      type = types.str;
      default = "taskd";
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
      default = "lattice-taskd";
      description = "Directory under /var/lib for persistent identity and state.";
    };

    repoPath = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Path to local Git task repository.";
    };

    ref = mkOption {
      type = types.str;
      default = "HEAD";
      description = "Git ref to reconcile.";
    };

    headSubject = mkOption {
      type = types.str;
      default = "git.head";
      description = "Meshbus subject for git.head requests.";
    };

    watchSubject = mkOption {
      type = types.str;
      default = "git.watch";
      description = "Meshbus subject for git.watch requests.";
    };

    agentSubject = mkOption {
      type = types.str;
      default = "agent.run";
      description = "Meshbus subject for agent.run requests.";
    };

    watchTimeout = mkOption {
      type = types.str;
      default = "10s";
      description = "Long poll timeout for git.watch.";
    };
  };
}
