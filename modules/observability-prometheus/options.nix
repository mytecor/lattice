{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.observability-prometheus = {
    enable = mkEnableOption "the Lattice Prometheus observability collector";

    package = mkOption {
      type = types.package;
      default = pkgs.prometheus;
      defaultText = lib.literalExpression "pkgs.prometheus";
      description = "The Prometheus package to run.";
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address Prometheus binds for its web UI / API. Kept on loopback by
        default (F12: observability is non-public); exposing it is an explicit
        operator decision, never a default.
      '';
    };

    port = mkOption {
      type = types.port;
      description = "TCP port Prometheus listens on (loopback).";
    };

    scrapeInterval = mkOption {
      type = types.str;
      default = "15s";
      description = "Global scrape interval for all scrape jobs.";
    };

    evaluationInterval = mkOption {
      type = types.str;
      default = "15s";
      description = "Rule evaluation interval (unused until alerts, kept sensible).";
    };

    retentionTime = mkOption {
      type = types.nullOr types.str;
      default = "15d";
      description = "How long to retain samples in storage (e.g. 15d).";
    };

    stateDir = mkOption {
      type = types.str;
      default = "prometheus";
      description = ''
        Directory below /var/lib where Prometheus stores TSDB data.
        Persisted via the node's /persist (see node config).
      '';
    };

    # Constant `environment` label applied to every llm-gateway scrape sample.
    # Low cardinality (one value per deployment); lets dashboards scope by
    # environment without a high-cardinality label on the exporter side.
    gatewayEnvironment = mkOption {
      type = types.str;
      default = "homelab";
      description = ''
        Value of the constant `environment` label attached to every sample of
        the llm-gateway scrape job. One value per deployment; the f12-04
        dashboards expose it as the `environment` template variable.
      '';
    };

    extraScrapeConfigs = mkOption {
      type = types.listOf types.attrs;
      default = [ ];
      description = ''
        Additional Prometheus scrape_config entries beyond the default
        llm-gateway job. Useful when later services expose /metrics on the node.
      '';
    };
  };
}
