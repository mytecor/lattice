{ lib, rnshPackage, ... }:

let
  inherit (lib) mkOption types;
in
{
  options.lattice.rnsh = {
    enable = mkOption {
      type = types.bool;
      default = false;
      description = "Enable the rnsh listener service.";
    };

    package = mkOption {
      type = types.package;
      default = rnshPackage;
      defaultText = lib.literalExpression "rns-rs.packages.<system>.rnsh";
      description = "Package providing the rnsh binary.";
    };

    user = mkOption {
      type = types.str;
      default = "rns";
      description = "User that runs rnsh.";
    };

    group = mkOption {
      type = types.str;
      default = "rns";
      description = "Group that runs rnsh.";
    };

    homeDir = mkOption {
      type = types.str;
      default = "/var/lib/rnsh";
      description = "HOME directory for rnsh runtime files.";
    };

    configDir = mkOption {
      type = types.str;
      default = "/var/lib/rns";
      description = "Reticulum config directory passed via --config.";
    };

    identity = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Identity file passed via --identity.";
    };

    service = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Listener identity service name passed via --service.";
    };

    announcePeriod = mkOption {
      type = types.nullOr types.int;
      default = null;
      description = "Announce period in seconds passed via --announce.";
    };

    allowed = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Allowed initiator identity hashes; each value becomes --allowed.";
    };

    noAuth = mkOption {
      type = types.bool;
      default = false;
      description = "Allow any initiator identity via --no-auth.";
    };

    remoteCommandAsArgs = mkOption {
      type = types.bool;
      default = false;
      description = "Pass remote command as arguments to the configured command.";
    };

    noRemoteCommand = mkOption {
      type = types.bool;
      default = false;
      description = "Reject remote command lines.";
    };

    noId = mkOption {
      type = types.bool;
      default = false;
      description = "Do not identify to the listener.";
    };

    mirrorExit = mkOption {
      type = types.bool;
      default = false;
      description = "Mirror remote command exit code.";
    };

    base256 = mkOption {
      type = types.bool;
      default = false;
      description = "Print compact base256 display for hashes.";
    };

    timeout = mkOption {
      type = types.nullOr (types.oneOf [ types.int types.float ]);
      default = null;
      description = "Path/link/protocol timeout in seconds.";
    };

    verbose = mkOption {
      type = types.int;
      default = 0;
      description = "Number of -v flags.";
    };

    quiet = mkOption {
      type = types.int;
      default = 0;
      description = "Number of -q flags.";
    };

    command = mkOption {
      type = types.listOf types.str;
      default = [ ];
      example = [ "/bin/sh" ];
      description = "Optional local command appended after -- for listener sessions.";
    };

    extraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Extra raw rnsh arguments inserted before -- command.";
    };
  };
}
