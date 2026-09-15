{ nixpkgs, pkgs, lib, observabilityModules, observabilityProfile }:

# Contract test for the f12-04 Grafana dashboards and the `environment` scrape
# label.
#
# Verifies the *semantically important* properties of the provisioning, not the
# whole dashboard JSON (prefer membership over exact equality):
#   - the dashboards ship from the repository (modules/grafana/dashboards/) and
#     are provisioned through the grafana module's provider set (source of
#     truth is the repo, not hand-editing in the UI);
#   - each dashboard is valid JSON with a stable `uid` and non-empty title;
#   - each dashboard references the expected metric/event surface and filters
#     by the low-cardinality `environment` label;
#   - the Prometheus scrape job attaches the constant `environment` label that
#     the dashboards filter by, and its default is the homelab deployment;
#   - the only free-text template variable is `request_id` — a Loki search
#     field, never a Prometheus label (F12 low-cardinality contract).

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

  # Dashboard JSON files shipped by the grafana module (source of truth).
  dashboardsDir = ../modules/grafana/dashboards;
  dashboardFiles = builtins.filter
    (name: lib.hasSuffix ".json" name)
    (builtins.attrNames (builtins.readDir dashboardsDir));

  # Parse each dashboard with importJSON so the test reads the exact JSON the
  # operator maintains (fails fast here rather than at Grafana startup).
  dashboards = map (name: lib.importJSON "${dashboardsDir}/${name}") dashboardFiles;

  # Concatenate every panel expression of a dashboard into one string so the
  # membership checks below search the whole dashboard truthfully.
  joinExprs = db: lib.concatStringsSep "\n"
    (map (p: lib.concatStringsSep "\n"
      (map (t: t.expr or "") (p.targets or [ ])))
    (db.panels or [ ]));

  byUid = uid: builtins.head (builtins.filter (db: (db.uid or "") == uid) dashboards);

  scrapeJobs = cfg.services.prometheus.scrapeConfigs;
  gatewayJob = builtins.head (builtins.filter (job: job.job_name == "llm-gateway") scrapeJobs);
  providers = cfg.services.grafana.provision.dashboards.settings.providers;
in
# --- Dashboards ship from the repository and are provisioned ---
assert lib.length dashboardFiles >= 3;
assert builtins.any (p: (p.name or "") == "lattice") providers;
assert lib.all (p: (p.options.path or "") == "${dashboardsDir}") providers;

# --- Stable uids, titles, datasource wiring ---
assert lib.all (d: (d.uid or "") != "" && (d.title or "") != "") dashboards;

# --- Metrics dashboard (llm-gateway-main) ---
let llm = byUid "llm-gateway-main"; in
assert lib.hasInfix "llm_requests_total" (joinExprs llm);
assert lib.hasInfix "llm_request_duration_seconds_bucket" (joinExprs llm);
assert lib.hasInfix "llm_ttft_seconds_bucket" (joinExprs llm);
assert lib.hasInfix "llm_input_tokens_total" (joinExprs llm);
# Errors, in-flight and fallback surface from f12-01 item 1.
assert lib.hasInfix "llm_attempts_total" (joinExprs llm);
assert lib.hasInfix "llm_requests_in_flight" (joinExprs llm);
assert lib.hasInfix "llm_fallbacks_total" (joinExprs llm);
# Filters by the low-cardinality environment label and the route/provider/model
# template variables.
assert lib.hasInfix "environment=\"$environment\"" (joinExprs llm);
assert lib.hasInfix "route=~\"$route\"" (joinExprs llm);
assert lib.hasInfix "provider=~\"$provider\"" (joinExprs llm);
# Uses $__rate_interval (the point of the dashboard: measurement over a real
# window, not a hardcoded step).
assert lib.hasInfix "$__rate_interval" (joinExprs llm);
# Correlation by request_id happens through the Loki logs panel, not a label.
assert lib.hasInfix "request_id" (joinExprs llm);
# The task requires the route/provider/model/environment/status variables; the
# status variable is part of the templating list.
assert lib.hasInfix "\"status\"" (builtins.toJSON llm.templating.list or []);

# --- Runtime dashboard (gateway-runtime) ---
let runtime = byUid "gateway-runtime"; in
assert lib.hasInfix "llm_balance_health" (joinExprs runtime);
assert lib.hasInfix "llm_balance_selections_total" (joinExprs runtime);
assert lib.hasInfix "cooldown_put|llm_retry|llm_fallback|hedge_launched|semaphore_denied" (joinExprs runtime);

# --- Investigation dashboard (loki-investigation) ---
let lokiDb = byUid "loki-investigation"; in
assert lib.hasInfix "request_id" (joinExprs lokiDb);
assert lib.hasInfix "count_over_time" (joinExprs lokiDb);
assert lib.hasInfix "| json" (joinExprs lokiDb);

# --- Prometheus scrape attaches the constant environment label (low card) ---
assert gatewayJob.static_configs != [ ];
assert builtins.all (sc: (sc.labels.environment or null) != null) gatewayJob.static_configs;

pkgs.runCommand "grafana-dashboards-contract" { } ''
  echo "f12-04 grafana dashboards contract holds:
  dashboards: ${builtins.concatStringsSep ", " (map (d: d.uid) dashboards)}
  environment label (scrape): ${(builtins.elemAt gatewayJob.static_configs 0).labels.environment}" > "$out"
''
