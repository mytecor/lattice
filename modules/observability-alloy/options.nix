{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.observability-alloy = {
    enable = mkEnableOption "the Lattice Grafana Alloy log shipper (journald → Loki)";

    package = mkOption {
      type = types.package;
      default = pkgs.grafana-alloy;
      defaultText = lib.literalExpression "pkgs.grafana-alloy";
      description = "The Grafana Alloy package to run.";
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = ''
        Address Alloy's internal HTTP server (debug UI / /-/reload) binds.
        Loopback by default; nothing about Alloy needs to be public.
      '';
    };

    port = mkOption {
      type = types.port;
      description = "TCP port Alloy's internal HTTP server listens on (loopback).";
    };

    # Source of gateway log events.
    journalUnitFilter = mkOption {
      type = types.listOf types.str;
      default = [ "_SYSTEMD_UNIT=llm-gateway.service" ];
      description = ''
        systemd journal `KEY=VALUE` matchers that select which units Alloy
        reads from. Defaults to the llm-gateway service (F12: the gateway is
        the only producer of structured request events).
      '';
    };

    lokiUrl = mkOption {
      type = types.str;
      description = ''
        Base URL of the target Loki instance (loopback). Alloy pushes to
        /loki/api/v1/push on this address.
      '';
    };

    # Structured metadata fields to preserve from the JSON payload. These stay
    # as non-indexed searchable fields (NOT as labels) per the F12 low
    # cardinality contract.
    structuredMetadataFields = mkOption {
      type = types.listOf types.str;
      default = [
        "request_id"
        "route"
        "route_stage"
        "status_code"
        "error_type"
        "provider"
        "model"
        "attempts"
        "event"
      ];
      description = ''
        JSON fields extracted into Loki structured metadata for searching by
        request_id / route / outcome. Deliberately not promoted to labels —
        cardinality stays bounded.
      '';
    };
  };
}
