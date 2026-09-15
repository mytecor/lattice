{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.grafana = {
    enable = mkEnableOption "the Lattice Grafana dashboard server (loopback)";

    package = mkOption {
      type = types.package;
      default = pkgs.grafana;
      defaultText = lib.literalExpression "pkgs.grafana";
      description = "The Grafana package to run.";
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address Grafana binds. Loopback by default (F12: observability is
        not public); external access is an explicit operator decision.
      '';
    };

    port = mkOption {
      type = types.port;
      description = "TCP port Grafana listens on (loopback).";
    };

    domain = mkOption {
      type = types.str;
      default = "localhost";
      description = "The public-facing domain for Grafana (affects root_url).";
    };

    adminUser = mkOption {
      type = types.str;
      default = "admin";
      description = "Initial admin username for Grafana.";
    };

    # File provider backed by an agenix secret. Grafana reads the password at
    # startup through the `${__file:/path}` provider so it never lands in the
    # Nix store (the nixpkgs module warns about plaintext otherwise).
    adminPasswordFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (e.g. an agenix secret) to a file containing the Grafana
        admin password. Mounted via the file provider; not read from argv or
        the store.
      '';
    };

    # Grafana 26.05 requires an explicit secret_key (no default); a stable,
    # operator-owned value is provided through the same file-provider mechanism
    # so it also never lands in the store.
    secretKeyFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (e.g. an agenix secret) to a file containing the Grafana
        secret_key (generated with `openssl rand -hex 32`). Required as of
        NixOS 26.05; supplied via the file provider.
      '';
    };

    dataDir = mkOption {
      type = types.path;
      default = "/var/lib/grafana";
      description = "Persistent Grafana data directory.";
    };

    prometheusUrl = mkOption {
      type = types.str;
      description = "Base URL of the Prometheus datasource (loopback).";
    };

    lokiUrl = mkOption {
      type = types.str;
      description = "Base URL of the Loki datasource (loopback).";
    };

    dashboardProviders = mkOption {
      type = types.listOf types.attrs;
      default = [ ];
      description = ''
        Grafana dashboard providers (provisioning). Each entry becomes a
        `providers` provisioning file which watches a directory of dashboard
        JSON files. Definitions live in the repository, not hand-edited in the
        UI (f12-04).
      '';
    };
  };
}
