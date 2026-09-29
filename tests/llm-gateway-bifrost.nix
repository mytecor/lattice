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
          clientKeys = [
            {
              id = "primary";
              secretFile = "/run/agenix/llm-gateway-client-key";
            }
          ];
          providers = {
            proxy = {
              id = "gonka-proxy";
              inferenceUrl = "https://proxy.gonka.invalid/v1";
              apiKeySecretFile = "/run/agenix/llm-provider-proxy";
              apiKeyEnv = "LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY";
              priority = 20;
            };
            openbroker = {
              id = "gonka-openbroker";
              inferenceUrl = "https://openbroker.gonka.invalid/v1";
              modelsUrl = "https://proxy.gonka.invalid/v1/models";
              apiKeySecretFile = "/run/agenix/llm-provider-openbroker";
              apiKeyEnv = "LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY";
              priority = 10;
            };
            vertex = {
              id = "google-vertex";
              baseProvider = "vertex";
              inferenceUrl = "https://aiplatform.googleapis.com";
              vertexCredentialsSecretFile = "/run/agenix/llm-provider-google-vertex-credentials";
              vertexCredentialsEnv = "LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS";
              vertexProjectId = "mytecor";
              vertexRegion = "global";
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
  envUnit = config.systemd.services.llm-gateway-env;
  envCredentials = envUnit.serviceConfig.LoadCredential;
in
assert config.lattice.llm-gateway.package == pkgs.lattice.llm-gateway;
# The gateway itself receives secrets only as environment variables: it has no
# LoadCredential and reads the EnvironmentFile produced by the env unit.
assert (service.serviceConfig.LoadCredential or [ ]) == [ ];
# EnvironmentFile may stay a raw string before systemd unit generation
# (NixOS coerces it to a list later), so accept either shape.
assert
  (if builtins.isString (service.serviceConfig.EnvironmentFile or "") then
    service.serviceConfig.EnvironmentFile == "/run/llm-gateway-env/keys.env"
  else
    builtins.elem "/run/llm-gateway-env/keys.env" (service.serviceConfig.EnvironmentFile or [ ]));
assert builtins.elem "llm-gateway-env.service" service.after;
# The env unit loads every declared secret under its config-declared env name.
assert builtins.elem "LATTICE_CLIENT_PRIMARY_KEY:/run/agenix/llm-gateway-client-key" envCredentials;
assert builtins.elem "LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY:/run/agenix/llm-provider-proxy" envCredentials;
assert builtins.elem "LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY:/run/agenix/llm-provider-openbroker" envCredentials;
assert builtins.elem "LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS:/run/agenix/llm-provider-google-vertex-credentials" envCredentials;
assert builtins.elem "llm-gateway.service" envUnit.requiredBy;
assert builtins.elem "llm-gateway.service" envUnit.partOf;
assert builtins.elem "llm-gateway.service" envUnit.before;
# The oneshot writes keys.env into its RuntimeDirectory; systemd removes that
# directory when the oneshot deactivates. Without RuntimeDirectoryPreserve the
# file is deleted the instant the unit finishes and llm-gateway.service fails
# to load its EnvironmentFile ("Failed to load environment files: No such file
# or directory") and never starts.
assert envUnit.serviceConfig.RuntimeDirectoryPreserve == "yes";
# preStart no longer assembles secrets with jq: it only copies the now-public
# (env-name-only) config template into the runtime directory.
assert !(lib.hasInfix ".providers |= map" service.preStart);
assert lib.hasInfix "--config /run/llm-gateway/config.json serve" service.serviceConfig.ExecStart;
assert !service.serviceConfig.MemoryDenyWriteExecute;
assert service.serviceConfig.NoNewPrivileges;
assert service.serviceConfig.ProtectSystem == "strict";
pkgs.runCommand "llm-gateway-bifrost-module-evaluation" { nativeBuildInputs = [ pkgs.jq pkgs.bash ]; } ''
  # Regression: the env-materializer's credentials list must be a single quoted
  # assignment (`creds="a b c"`), not `creds=a b c` — bash reads the latter as
  # `creds=a` followed by running `b` as a command, failing the unit with exit
  # 127 so llm-gateway never starts and Caddy serves 502 for the service. The
  # generated script must both carry the quoted form and be valid bash.
  env_script=${envUnit.serviceConfig.ExecStart}
  if ! grep -Eq '^\s*creds="[^"]+"\s*$' "$env_script"; then
    echo "llm-gateway-env: creds assignment is not a single quoted string; " >&2
    echo "unit would fail with 'command not found' and llm-gateway stays down" >&2
    exit 1
  fi
  if ! ${pkgs.bash}/bin/bash -n "$env_script"; then
    echo "llm-gateway-env: generated script is not valid bash" >&2
    exit 1
  fi
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

  # Provider credentials are env-var references, one common key per provider
  # (no separate models key), and the client keys carry only non-secret ids.
  if ! jq -e 'any(.providers[]; .id == "gonka-proxy" and .api_key == "env.LATTICE_LLM_PROVIDER_GONKA_PROXY_KEY")' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "gonka-proxy env api_key reference missing" >&2
    exit 1
  fi
  if ! jq -e 'any(.providers[]; .id == "gonka-openbroker" and .api_key == "env.LATTICE_LLM_PROVIDER_GONKA_OPENBROKER_KEY")' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "gonka-openbroker env api_key reference missing" >&2
    exit 1
  fi
  if ! jq -e 'any(.providers[]; .id == "google-vertex" and .base_provider == "vertex" and .vertex_project_id == "mytecor" and .vertex_region == "global" and .vertex_auth_credentials == "env.LATTICE_LLM_PROVIDER_GOOGLE_VERTEX_CREDENTIALS")' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "Vertex provider resource configuration missing" >&2
    exit 1
  fi
  # The Go ClientKey decoder is strict and its credential field is `key`.
  # Using provider-style `api_key` makes the service reject the generated
  # config at startup, which in turn aborts every NixOS activation.
  if ! jq -e '.client_api_keys == [ { "id": "primary", "key": "env.LATTICE_CLIENT_PRIMARY_KEY" } ]' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "client_api_keys env references missing" >&2
    exit 1
  fi
  if jq -e 'any(.providers[]; .models_api_key != null)' ${config.lattice.llm-gateway.publicConfigFile} >/dev/null; then
    echo "models_api_key must not exist (one common key per provider)" >&2
    exit 1
  fi

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

  # No credential paths, secret values or secret env values land in the public
  # config: only env-var NAMES do.
  if grep -q 'llm-gateway-client-key\|llm-provider-proxy\|llm-provider-openbroker\|llm-provider-google-vertex-credentials' ${config.lattice.llm-gateway.publicConfigFile}; then
    echo "public Bifrost proxy config contains a credential path" >&2
    exit 1
  fi
  touch "$out"
''
