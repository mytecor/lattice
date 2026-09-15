{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.observability-loki;
in
{
  config = lib.mkIf cfg.enable {
    services.loki = {
      enable = true;
      package = cfg.package;
      dataDir = toString cfg.dataDir;
      # Single-binary, loopback, in-memory ring: F12 wants exactly one Loki on
      # the node, no clustering, no memberlist chatter. Filesystem storage
      # under the node's /persist.
      configuration = {
        auth_enabled = false;
        server = {
          http_listen_address = cfg.listenAddress;
          http_listen_port = cfg.port;
        };
        common = {
          instance_addr = cfg.listenAddress;
          path_prefix = toString cfg.dataDir;
          storage = {
            filesystem = {
              chunks_directory = "${toString cfg.dataDir}/chunks";
              rules_directory = "${toString cfg.dataDir}/rules";
            };
          };
          replication_factor = 1;
          ring = {
            kvstore = { store = "inmemory"; };
          };
        };
        schema_config = {
          configs = [{
            from = "2020-10-24";
            store = "tsdb";
            object_store = "filesystem";
            schema = "v13";
            index = {
              prefix = "index_";
              period = "24h";
            };
          }];
        };
        limits_config = {
          retention_period = cfg.retentionPeriod;
          # Structured metadata (non-indexed fields) is what lets Alloy ship
          # request_id/event/status as searchable fields while keeping labels
          # low-cardinality. Enabled so the f12-03 DoD (find by request_id)
          # actually works.
          allow_structured_metadata = true;
        };
        # Retention only takes effect when the compactor runs; single-binary
        # mode runs it on the same node. Without retention_enabled the
        # retention_period is a no-op and /var/lib/loki grows without bound.
        compactor = {
          working_directory = "${toString cfg.dataDir}/compactor";
          retention_enabled = true;
        };
      };
    };

    # The nixpkgs loki unit already runs ProtectiveSystem=full, PrivateTmp,
    # DevicePolicy=closed and NoNewPrivileges. Nothing to override; the single
    # node store exposes no network beyond loopback (http_listen_address).
  };
}
