{ nixpkgs, pkgs, gatewayModule, gatewayProfile }:

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      gatewayModule
      gatewayProfile
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway = {
          credentials = {
            LATTICE_CLIENT_PRIMARY_KEY = "/run/agenix/llm-gateway-client-key";
            LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY = "/run/agenix/llm-provider-openbroker";
            LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY = "/run/agenix/llm-provider-proxy";
            LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS = "/run/agenix/llm-provider-google-vertex-credentials";
          };
          settings = {
            client_api_keys = [{ id = "primary"; key = "env.LATTICE_CLIENT_PRIMARY_KEY"; }];
            providers = [
              {
                id = "gonka-proxy";
                base_provider = "openai";
                inference_url = "https://proxy.gonka.invalid/v1";
                api_key = "env.LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY";
                priority = 20;
              }
              {
                id = "gonka-openbroker";
                base_provider = "openai";
                inference_url = "https://openbroker.gonka.invalid/v1";
                models_url = "https://proxy.gonka.invalid/v1/models";
                api_key = "env.LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY";
                priority = 10;
              }
              {
                id = "google-vertex";
                base_provider = "vertex";
                inference_url = "https://aiplatform.googleapis.com";
                vertex_auth_credentials = "env.LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS";
                vertex_project_id = "mytecor";
                vertex_region = "global";
              }
            ];
            routing_rules = [
              { route = "standard"; action = "filter"; where.model.eq = "standard"; }
              { route = "standard"; action = "filter"; where.provider."in" = [ "gonka-proxy" "gonka-openbroker" ]; }
              { route = "standard"; action = "map"; native = "native-model"; }
              { route = "standard"; action = "rank"; strategy = "priority"; }
              { route = "standard"; action = "balance"; strategy = "weighted"; weights = { gonka-proxy = 2; gonka-openbroker = 1; }; window = "5m"; error_budget = 0.2; }
              { route = "standard"; action = "race"; count = 1; }
              { route = "standard"; action = "semaphore"; max_calls = 4; max_in_flight = 3; max_calls_per_provider = 1; }
            ];
          };
        };
      }
    ];
  }).config;

  service = config.systemd.services.llm-gateway;
  envUnit = config.systemd.services.llm-gateway-env;
  envCredentials = envUnit.serviceConfig.LoadCredential;
in
assert config.lattice.llm-gateway.package == pkgs.lattice.llm-gateway;
assert (service.serviceConfig.LoadCredential or [ ]) == [ ];
assert
  (if builtins.isString (service.serviceConfig.EnvironmentFile or "") then
    service.serviceConfig.EnvironmentFile == "/run/llm-gateway-env/keys.env"
  else
    builtins.elem "/run/llm-gateway-env/keys.env" (service.serviceConfig.EnvironmentFile or [ ]));
assert builtins.elem "llm-gateway-env.service" service.after;
assert builtins.elem "LATTICE_CLIENT_PRIMARY_KEY:/run/agenix/llm-gateway-client-key" envCredentials;
assert builtins.elem "LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY:/run/agenix/llm-provider-proxy" envCredentials;
assert builtins.elem "LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY:/run/agenix/llm-provider-openbroker" envCredentials;
assert builtins.elem "LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS:/run/agenix/llm-provider-google-vertex-credentials" envCredentials;
assert builtins.elem "llm-gateway.service" envUnit.requiredBy;
assert builtins.elem "llm-gateway.service" envUnit.partOf;
assert builtins.elem "llm-gateway.service" envUnit.before;
assert envUnit.serviceConfig.RuntimeDirectoryPreserve == "yes";
assert lib.hasInfix "--config /run/llm-gateway/config.json serve" service.serviceConfig.ExecStart;
assert !service.serviceConfig.MemoryDenyWriteExecute;
assert service.serviceConfig.NoNewPrivileges;
assert service.serviceConfig.ProtectSystem == "strict";

pkgs.runCommand "llm-gateway-bifrost-module-evaluation" {
  nativeBuildInputs = [ pkgs.jq pkgs.bash ];
} ''
  env_script=${envUnit.serviceConfig.ExecStart}
  if ! grep -Eq '^\s*creds="[^"]+"\s*$' "$env_script"; then
    echo "llm-gateway-env: credential list is not one quoted assignment" >&2
    exit 1
  fi
  ${pkgs.bash}/bin/bash -n "$env_script"

  cfg=${config.lattice.llm-gateway.publicConfigFile}
  jq -e '
    .host == "127.0.0.1"
    and .port == 9208
    and .metrics_host == "127.0.0.1"
    and .metrics_port == 9209
    and .client_api_keys == [{"id":"primary","key":"env.LATTICE_CLIENT_PRIMARY_KEY"}]
    and any(.providers[];
      .id == "gonka-proxy"
      and .api_key == "env.LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY")
    and any(.providers[];
      .id == "google-vertex"
      and .vertex_auth_credentials == "env.LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS")
    and any(.routing_rules[];
      .action == "semaphore"
      and .max_calls == 4
      and .max_in_flight == 3)
  ' "$cfg" >/dev/null

  if grep -q 'llm-gateway-client-key\|llm-provider-proxy\|llm-provider-openbroker\|llm-provider-google-vertex-credentials' "$cfg"; then
    echo "public gateway config contains a credential path" >&2
    exit 1
  fi

  touch "$out"
''
