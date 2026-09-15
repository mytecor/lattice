{ nixpkgs, pkgs, lib, observabilityModules, observabilityProfile }:

# Contract test for the F12 observability stack modules
# (observability-prometheus, observability-loki, observability-alloy,
# grafana + profiles/observability).
#
# Verifies the *contracts* of the modules in isolation (never the production
# node values): that enabling the stack activates the right upstream services,
# keeps them loopback-only and non-public, wires the expected pipeline
# (Prometheus scrape / Alloy journal → Loki → Grafana datasources), uses an
# agenix-backed admin secret for Grafana, and hardens the units — not that
# specific ports/names happen to match current config.

let
  cfg = (nixpkgs.lib.nixosSystem {
    modules = observabilityModules ++ [
      observabilityProfile
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.grafana.adminPasswordFile = "/run/agenix/grafana-admin-password";
        lattice.grafana.secretKeyFile = "/run/agenix/grafana-secret-key";
      }
    ];
  }).config;

  prometheusSvc = cfg.systemd.services.prometheus;
  lokiSvc = cfg.systemd.services.loki;
  alloySvc = cfg.systemd.services.alloy;
  grafanaSvc = cfg.systemd.services.grafana;
  alloyConfig = cfg.environment.etc."alloy/config.alloy".text or "";

  scrapeJobs = cfg.services.prometheus.scrapeConfigs;
  grafanaDatasources = cfg.services.grafana.provision.datasources.settings.datasources;

  # One scrape job must target the llm-gateway metrics endpoint.
  gatewayJob = builtins.head (builtins.filter (job: job.job_name == "llm-gateway") scrapeJobs);
in
assert cfg.services.prometheus.enable;
assert cfg.services.loki.enable;
assert cfg.services.alloy.enable;
assert cfg.services.grafana.enable;

# --- F12: non-public by design ---
assert cfg.services.prometheus.listenAddress == "127.0.0.1";
assert cfg.services.loki.configuration.server.http_listen_address == "127.0.0.1";
assert cfg.services.grafana.settings.server.http_addr == "127.0.0.1";

# --- Prometheus scrapes the llm-gateway metrics endpoint (loopback, /metrics) ---
assert gatewayJob.job_name == "llm-gateway";
assert gatewayJob.metrics_path == "/metrics";
assert builtins.any
  (sc: builtins.any (t: t == "127.0.0.1:9209") (sc.targets or [ ]))
  gatewayJob.static_configs;

# --- Alloy reads the llm-gateway journal unit and pushes to Loki ---
# request_id is extracted as structured metadata (searchable field), but never
# promoted to a label (F12 low-cardinality contract).
assert builtins.hasAttr "alloy/config.alloy" cfg.environment.etc;
assert lib.hasInfix "loki.source.journal" alloyConfig;
assert lib.hasInfix "_SYSTEMD_UNIT=llm-gateway.service" alloyConfig;
assert lib.hasInfix "loki.write" alloyConfig;
assert lib.hasInfix "stage.structured_metadata" alloyConfig;

# --- Grafana datasources point at loopback Prometheus + Loki ---
assert builtins.any (ds: ds.type == "prometheus" && ds.uid == "prometheus") grafanaDatasources;
assert builtins.any (ds: ds.type == "loki" && ds.uid == "loki") grafanaDatasources;
assert builtins.all (ds: (ds.access or "proxy") == "proxy") grafanaDatasources;

# --- Grafana secrets come from file providers, never the store ---
assert lib.hasInfix "__file:" cfg.services.grafana.settings.security.admin_password;
assert lib.hasInfix "__file:" cfg.services.grafana.settings.security.secret_key;

# --- Sandbox: every unit hardened ---
assert prometheusSvc.serviceConfig.NoNewPrivileges or false;
assert lokiSvc.serviceConfig.NoNewPrivileges or false;
assert alloySvc.serviceConfig.NoNewPrivileges or false;
assert grafanaSvc.serviceConfig.NoNewPrivileges or false;
# Each unit runs under a strict systemd sandbox; upstream lattice wrappers
# inherit the nixpkgs sandbox (full for prometheus/loki/grafana) and the
# lattice module adds it for alloy (upstream is intentionally thin).
assert lib.elem (prometheusSvc.serviceConfig.ProtectSystem or "") [ "full" "strict" ];
assert lib.elem (lokiSvc.serviceConfig.ProtectSystem or "") [ "full" "strict" ];
assert lib.elem (alloySvc.serviceConfig.ProtectSystem or "") [ "full" "strict" ];
assert lib.elem (grafanaSvc.serviceConfig.ProtectSystem or "") [ "full" "strict" ];

pkgs.runCommand "observability-stack-contract" { } ''
  echo "F12 observability stack contract holds" > "$out"
''
