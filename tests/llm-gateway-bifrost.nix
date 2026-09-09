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
              accessGroup = "gonka";
              inferenceUrl = "https://proxy.gonka.invalid/v1";
              apiKeyFile = "/run/agenix/llm-provider-proxy";
              priority = 20;
            };
            openbroker = {
              id = "gonka-openbroker";
              accessGroup = "gonka";
              inferenceUrl = "https://openbroker.gonka.invalid/v1";
              modelsUrl = "https://proxy.gonka.invalid/v1/models";
              apiKeyFile = "/run/agenix/llm-provider-openbroker";
              modelsApiKeyFile = "/run/agenix/llm-provider-proxy";
              priority = 10;
            };
          };
          models = [
            { logical = "stupid"; accessGroup = "gonka"; native = "MiniMaxAI/MiniMax-M2.7"; }
            { logical = "standard"; accessGroup = "gonka"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
          ];
          routingRules = lib.concatMap (model: [
            { inherit model; action = "pool"; accessGroups = [ "gonka" ]; }
            { inherit model; action = "rank"; strategy = "priority"; }
            { inherit model; action = "race"; count = 2; }
            {
              inherit model;
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
  grep -q '"log_level":"silent"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"inference_url":"https://openbroker.gonka.invalid/v1"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"models_url":"https://proxy.gonka.invalid/v1/models"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"pool"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"rank"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"semaphore"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"max_calls":4' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"max_in_flight":3' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"count":2' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"strategy":"priority"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"affinity_file":"/run/llm-gateway/affinity.json"' ${config.lattice.llm-gateway.publicConfigFile}

  # access_groups must appear only on pool rules (no legacy race shorthand).
  if ! jq -e '[.routing_rules[] | select(((."access_groups" // []) | length) > 0) | .action] | all(. == "pool")' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "legacy access_groups outside pool rules" >&2
    exit 1
  fi

  if grep -q 'llm-gateway-client-key\|llm-provider-proxy\|llm-provider-openbroker' ${config.lattice.llm-gateway.publicConfigFile}; then
    echo "public Bifrost proxy config contains a credential path" >&2
    exit 1
  fi
  touch "$out"
''
