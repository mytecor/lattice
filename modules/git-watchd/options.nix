{ lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.git-watchd = {
    enable = mkEnableOption "git-watchd, the Git revision watcher and Meshbus adapter";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.git-watchd;
      defaultText = lib.literalExpression "pkgs.lattice.git-watchd";
      description = "The git-watchd package to run.";
    };

    user = mkOption {
      type = types.str;
      default = "git-watchd";
      description = "System user running the daemon.";
    };

    group = mkOption {
      type = types.str;
      default = "git-watchd";
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
      default = "lattice-git-watchd";
      description = "Directory under /var/lib for persistent identity and state.";
    };

    allowedDir = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Allowed directory prefix for local git repositories.";
    };

    headSubject = mkOption {
      type = types.str;
      default = "git.head";
      description = "Meshbus subject for head requests.";
    };

    watchSubject = mkOption {
      type = types.str;
      default = "git.watch";
      description = "Meshbus subject for watch requests.";
    };

    pollInterval = mkOption {
      type = types.str;
      default = "250ms";
      description = "Polling interval for git ref checking.";
    };
  };
}
