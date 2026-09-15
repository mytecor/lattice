{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.observability-loki = {
    enable = mkEnableOption "the Lattice Loki log store (single-binary, loopback)";

    package = mkOption {
      type = types.package;
      default = pkgs.grafana-loki;
      defaultText = lib.literalExpression "pkgs.grafana-loki";
      description = "The Grafana Loki package to run (single-binary mode).";
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address Loki binds for its HTTP API. Loopback by default (F12:
        observability is non-public); Alloy on the same node is the only
        writer.
      '';
    };

    port = mkOption {
      type = types.port;
      description = "TCP port Loki listens on (loopback).";
    };

    dataDir = mkOption {
      type = types.path;
      default = "/var/lib/loki";
      description = "Persistent directory for Loki chunks and index.";
    };

    retentionPeriod = mkOption {
      type = types.str;
      default = "720h"; # 30d
      description = "Loki retention period (e.g. 720h = 30 days).";
    };
  };
}
