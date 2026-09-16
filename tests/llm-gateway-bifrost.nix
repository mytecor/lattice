{ nixpkgs, pkgs, gatewayModule, gatewayProfile }:

let
  inherit (nixpkgs) lib;
  models = [ "stupid" "standard" ];
  config = (lib.nixosSystem {
    modules = [
      gatewayModule
      gatewayProfile
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway = {
          clientCredentialFile = "/run/agenix/llm-gateway-client-key";
          providers = {
            proxy = {
              id = "gonka-proxy";
              inferenceUrl = "https://proxy.gonka.invalid/v1";
              apiKeyFile = "/run/agenix/llm-provider-proxy";
              priority = 20;
            };
            openbroker = {
              id = "gonka-openbroker";
              inferenceUrl = "https://openbroker.gonka.invalid/v1";
              modelsUrl = "https://proxy.gonka.invalid/v1/models";
              apiKeyFile = "/run/agenix/llm-provider-openbroker";
              modelsApiKeyFile = "/run/agenix/llm-provider-proxy";
              priority = 10;
            };
          };
          routingRules = lib.concatMap (model: [
            { route = model; action = "filter"; where = { model = { eq = model; }; }; }
            {
              route = model;
              action = "filter";
              where = { provider = { "in" = [ "gonka-proxy" "gonka-openbroker" ]; }; };
            }
            {
              route = model;
              action = "map";
              native = if model == "standard" then "deepseek-ai/DeepSeek-V4-Flash-0731" else "MiniMaxAI/MiniMax-M2.7";
            }
            { route = model; action = "rank"; strategy = "priority"; }
            {
              route = model;
              action = "balance";
              strategy = "adaptive";
              weights = { gonka-proxy = 2; gonka-openbroker = 1; };
              window = "5m";
              errorBudget = 0.2;
            }
            { route = model; action = "race"; count = 1; }
            {
              route = model;
              action = "semaphore";
              maxCalls = 4;
              maxInFlight = 3;
              maxCallsPerProvider = 1;
            }
          ]) models;
        };
      }
    ];
  }).config;

  service = config.systemd.services.llm-gateway;
  credentials = service.serviceConfig.LoadCredential;
in
assert config.lattice.llm-gateway.package == pkgs.lattice.llm-gateway;
assert builtins.elem "client-key:/run/agenix/llm-gateway-client-key" credentials;
assert builtins.elem "provider-proxy-api-key:/run/agenix/llm-provider-proxy" credentials;
assert builtins.elem "provider-openbroker-models-api-key:/run/agenix/llm-provider-proxy" credentials;
assert builtins.elem "provider-openbroker-api-key:/run/agenix/llm-provider-openbroker" credentials;
assert lib.hasInfix ".providers |= map" service.preStart;
assert lib.hasInfix ".[$field] = $secret" service.preStart;
assert lib.hasInfix "--config /run/llm-gateway/config.json serve" service.serviceConfig.ExecStart;
assert !service.serviceConfig.MemoryDenyWriteExecute;
assert service.serviceConfig.NoNewPrivileges;
assert service.serviceConfig.ProtectSystem == "strict";
pkgs.runCommand "llm-gateway-bifrost-module-evaluation" { nativeBuildInputs = [ pkgs.jq ]; } ''
  grep -q '"catalog_refresh_interval":"10m"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"stream_idle_timeout":"5m"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"log_level":"silent"' ${config.lattice.llm-gateway.publicConfigFile}
  # Metrics listener: loopback by default, distinct port from the API listener.
  grep -q '"metrics_host":"127.0.0.1"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"metrics_port":9209' ${config.lattice.llm-gateway.publicConfigFile}
  if jq -e '.port == .metrics_port' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "metrics port must not equal the API port" >&2
    exit 1
  fi
  if jq -e '.metrics_host != "127.0.0.1" and .metrics_host != "::1"' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "metrics host must be loopback by default (non-public endpoint)" >&2
    exit 1
  fi
  grep -q '"inference_url":"https://openbroker.gonka.invalid/v1"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"models_url":"https://proxy.gonka.invalid/v1/models"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"filter"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"map"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"rank"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"semaphore"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"max_calls":4' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"max_in_flight":3' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"count":1' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"strategy":"priority"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"balance"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"strategy":"adaptive"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"error_budget":0.2' ${config.lattice.llm-gateway.publicConfigFile}
  # weights is a JSON object: key order is not stable (builtins.toJSON sorts
  # attribute names), so assert on the object content, not on a textual
  # representation whose ordering a config change may legitimately alter.
  if ! jq -e 'any(.routing_rules[]; (.action == "balance") and (.weights == { "gonka-proxy": 2, "gonka-openbroker": 1 }))' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "expected adaptive balance weights (gonka-proxy=2, gonka-openbroker=1) not found" >&2
    exit 1
  fi
  grep -q '"affinity_file":"/run/llm-gateway/affinity.json"' ${config.lattice.llm-gateway.publicConfigFile}

  # Every filter provider action must carry explicit provider ids and every
  # map action exactly a native id (no provider list: provider selection lives
  # exclusively in the filter). No access_groups remain anywhere.
  if ! jq -e '.routing_rules | all(if .action == "filter" and (.where | has("provider")) then ((.where.provider["in"] | type) == "array" and (.where.provider["in"] | length) > 0) else true end)' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "a filter provider action lacks an in list" >&2
    exit 1
  fi
  if ! jq -e '.routing_rules | all(if .action == "map" then ((.native | type) == "string") and (((.providers // null) == null) or (.providers | type) != "array") else true end)' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "a map action lacks native or still carries providers" >&2
    exit 1
  fi
  if ! jq -e '.routing_rules | all(.action == "filter" or (.route | type) == "string")' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "a routing rule lacks a named route" >&2
    exit 1
  fi
  if jq -e 'any(.routing_rules[]; has("access_groups")) or has("models")' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "legacy access_groups/models remain in the routing contract" >&2
    exit 1
  fi
  # Logical models must be derived from the rules (no separate models key).
  if jq -e '(.models // null) != null' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "public config still carries a separate models registry" >&2
    exit 1
  fi

  if grep -q 'llm-gateway-client-key\|llm-provider-proxy\|llm-provider-openbroker' ${config.lattice.llm-gateway.publicConfigFile}; then
    echo "public Bifrost proxy config contains a credential path" >&2
    exit 1
  fi
  touch "$out"
''
