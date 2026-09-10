{ lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.pi-acp-daemon = {
    enable = mkEnableOption "persistent multi-session Pi ACP daemon";

    hydraPackage = mkOption {
      type = types.package;
      default = pkgs.lattice.hydra-acp;
      description = "Pinned hydra-acp daemon and shim package.";
    };

    agentPackage = mkOption {
      type = types.package;
      default = pkgs.lattice.pi-acp;
      description = "Pinned pi-acp adapter spawned for every Hydra session.";
    };

    user = mkOption {
      type = types.str;
      default = "root";
      description = "Existing user that owns Hydra state and spawned Pi processes.";
    };

    group = mkOption {
      type = types.str;
      default = "root";
      description = "Existing group that owns Hydra state and spawned Pi processes.";
    };

    host = mkOption {
      type = types.enum [ "127.0.0.1" "::1" ];
      default = "127.0.0.1";
      description = "Loopback-only Hydra listen address.";
    };

    port = mkOption {
      type = types.port;
      default = 55514;
      description = "Hydra loopback listen port.";
    };

    logLevel = mkOption {
      type = types.enum [ "debug" "info" "warn" "error" ];
      default = "info";
      description = "Hydra daemon log level.";
    };

    sessionIdleTimeoutSeconds = mkOption {
      type = types.ints.unsigned;
      default = 0;
      description = "Idle lifetime in seconds; zero keeps interactive sessions warm.";
    };

    stateDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "hydra-acp";
      description = "systemd StateDirectory name below /var/lib.";
    };

    runtimeDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "hydra-acp";
      description = "systemd RuntimeDirectory name below /run.";
    };

    internalToken = mkOption {
      type = types.strMatching "[A-Za-z0-9._~-]+";
      default = "lattice-loopback-no-auth";
      description = ''
        Public token used only for the mandatory Caddy-to-Hydra loopback handshake. It is not an
        access-control credential while the LAN endpoint intentionally has no authentication.
      '';
    };

    serviceName = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9-]*";
      default = "acp";
      description = "Service label used for the Caddy and mDNS hostname.";
    };

    generatedConfigFile = mkOption {
      type = types.path;
      readOnly = true;
      description = "Secret-free Hydra config generated in the Nix store.";
    };
  };
}
