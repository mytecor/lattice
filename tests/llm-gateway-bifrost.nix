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
              inferenceUrl = "https://proxy.gonka.invalid";
              apiKeyFile = "/run/agenix/llm-provider-proxy";
              priority = 20;
            };
            openbroker = {
              id = "gonka-openbroker";
              accessGroup = "gonka";
              inferenceUrl = "https://openbroker.gonka.invalid";
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
            { inherit model; action = "race"; providers = [ "gonka-proxy" "gonka-openbroker" ]; }
            { inherit model; action = "retry"; attempts = 10; on = [ "429" "5xx" "timeout" "connection_error" ]; }
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
pkgs.runCommand "llm-gateway-bifrost-module-evaluation" { } ''
  grep -q '"catalog_refresh_interval":"10m"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"inference_url":"https://openbroker.gonka.invalid"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"models_url":"https://proxy.gonka.invalid/v1/models"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"action":"race"' ${config.lattice.llm-gateway.publicConfigFile}
  grep -q '"attempts":10' ${config.lattice.llm-gateway.publicConfigFile}

  if grep -q 'llm-gateway-client-key\|llm-provider-proxy\|llm-provider-openbroker' ${config.lattice.llm-gateway.publicConfigFile}; then
    echo "public Bifrost proxy config contains a credential path" >&2
    exit 1
  fi
  touch "$out"
''
