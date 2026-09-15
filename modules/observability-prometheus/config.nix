{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.observability-prometheus;
  gatewayMetricsPort = config.lattice.llm-gateway.metricsPort or 9209;
  gatewayEnable = config.lattice.llm-gateway.enable or false;
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = !gatewayEnable || gatewayMetricsPort != null;
        message = ''
          lattice.observability-prometheus: the llm-gateway module must expose
          a metricsPort to scrape (the default is 127.0.0.1:9209). Disable
          Prometheus or set lattice.llm-gateway.metricsPort.
        '';
      }
    ];

    services.prometheus = {
      enable = true;
      package = cfg.package;
      listenAddress = cfg.listenAddress;
      port = cfg.port;
      stateDir = cfg.stateDir;
      retentionTime = cfg.retentionTime;
      globalConfig = {
        scrape_interval = cfg.scrapeInterval;
        evaluation_interval = cfg.evaluationInterval;
      };
      # F12 low-cardinality: single job for the llm-gateway metrics endpoint.
      # The target is loopback, and the job is relabelled to a constant
      # `service="llm-gateway"` label so dashboards can filter uniformly.
      scrapeConfigs = [
        {
          job_name = "llm-gateway";
          scrape_interval = cfg.scrapeInterval;
          metrics_path = "/metrics";
          static_configs = [{
            targets = [ "${cfg.listenAddress}:${toString gatewayMetricsPort}" ];
            labels = {
              service = "llm-gateway";
            };
          }];
        }
      ] ++ cfg.extraScrapeConfigs;
    };

    # The nixpkgs prometheus unit already ships the strict sandbox
    # (ProtectSystem=full, PrivateUsers, DevicePolicy=strict,
    # MemoryDenyWriteExecute=true, SystemCallFilter). Nothing to override.
  };
}
