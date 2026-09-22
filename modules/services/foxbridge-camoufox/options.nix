{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  # F18: the long-running browser runtime — Camoufox (anti-detect Firefox)
  # driven through Foxbridge (the CDP→Juggler protocol proxy). Loopback-only
  # CDP endpoint; the Jev agent is the only consumer, via BU_CDP_URL.
  #
  # Option prefix follows the repo-wide `lattice.<service>` convention (all 14
  # existing modules), not the `services.*` sketch in the original f18-08 task
  # framing — the flake registers it under `self.nixosModules.foxbridge-camoufox`.
  options.lattice.foxbridge-camoufox = {
    enable = mkEnableOption "the Foxbridge + Camoufox browser runtime (F18)";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.foxbridge;
      defaultText = lib.literalExpression "pkgs.lattice.foxbridge";
      description = "The Foxbridge CDP proxy package to run.";
    };

    camoufoxPackage = mkOption {
      type = types.package;
      default = pkgs.lattice.camoufox;
      defaultText = lib.literalExpression "pkgs.lattice.camoufox";
      description = "The Camoufox (anti-detect Firefox) package Foxbridge launches.";
    };

    user = mkOption {
      type = types.str;
      default = "foxbridge";
      description = "System user running the browser runtime.";
    };

    group = mkOption {
      type = types.str;
      default = "foxbridge";
      description = "System group of the runtime user.";
    };

    # Foxbridge hardcodes 127.0.0.1 as the CDP bind address (pkg/cdp/server.go
    # sets host = "127.0.0.1" and main.go never calls SetHost), so this option
    # documents the loopback-only security contract explicitly rather than
    # controlling the actual socket. A config asserting otherwise is refused.
    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        CDP listen address. Foxbridge itself binds the loopback address
        (127.0.0.1) by construction and exposes no CLI flag to change it, so
        this option is a *contract* declared for the stack: it must stay on
        loopback, and any other value is rejected by a module assertion. The
        CDP endpoint is never published outside the host.
      '';
    };

    port = mkOption {
      type = types.port;
      default = 9222;
      description = "TCP port Foxbridge serves the CDP WebSocket endpoint on (loopback).";
    };

    camoufox = {
      headless = mkOption {
        type = types.bool;
        default = true;
        description = "Run Camoufox headless (no X display).";
      };

      humanize = mkOption {
        type = types.bool;
        default = true;
        description = ''
          Enable Camoufox `humanize` (natural mouse trajectories and delays),
          delivered as CAMOU_CONFIG_1='{"humanize":true}' — the env-var config
          channel Camoufox reads (small config fits a single 32767-byte chunk).
          Verified in f18-06: real cursor path, fingerprint unchanged.
        '';
      };

      profileDir = mkOption {
        type = types.path;
        default = "/run/foxbridge-camoufox/profile";
        description = "Disposable Firefox profile dir passed to Foxbridge via --profile (tmpfs).";
      };
    };
  };
}
