{ nixpkgs, pkgs, gatewayModule }:

let
  inherit (nixpkgs) lib;

  # The flat routing contract (f7-12): `routingRules` is the single source of
  # truth. There is no `models`/`pipeline` sugar (removed f7-14): every entry
  # route, transition subroute and fallback is written by hand and validated
  # by the shared typed `rewriteRule` evaluator during Nix evaluation.
  config = (lib.nixosSystem {
    modules = [
      gatewayModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway.enable = true;
        lattice.llm-gateway = {
          providers = {
            proxy = {
              id = "gonka-proxy";
              inferenceUrl = "https://proxy.gonka.invalid/v1";
              apiKeyFile = "/run/agenix/llm-provider-proxy";
              priority = 20;
              stripParams = [ "thinking" "reasoning_effort" ];
              setParams = {
                thinking = { type = "disabled"; };
              };
            };
            openbroker = {
              id = "gonka-openbroker";
              inferenceUrl = "https://openbroker.gonka.invalid/v1";
              apiKeyFile = "/run/agenix/llm-provider-openbroker";
              priority = 10;
            };
          };
          # Explicit flat routing for two logical models, each with its own
          # entry pipeline, retry/hedge subroutes and a fallback — the same
          # shape the removed sugar used to generate, now written by hand.
          routingRules = [
            # --- standard (DeepSeek): entry route races 2 providers ---
            { route = "standard"; action = "filter"; where = { model = { eq = "standard"; }; }; }
            { route = "standard"; action = "filter"; where = { provider = { "in" = [ "gonka-proxy" "gonka-openbroker" ]; }; }; }
            { route = "standard"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
            { route = "standard"; action = "rank"; strategy = "priority"; }
            { route = "standard"; action = "balance"; strategy = "p2c"; }
            {
              route = "standard";
              action = "affinity";
              sources = [ "responses.conversation" "responses.previous_response_id" ];
              ttl = "24h";
              onMissing = "ignore";
              onProviderFailure = "fail-closed";
            }
            { route = "standard"; action = "race"; count = 1; }
            { route = "standard"; action = "retry"; target = "standard.retry"; attempts = 2; }
            { route = "standard"; action = "semaphore"; maxCalls = 4; maxInFlight = 3; maxCallsPerProvider = 1; }
            { route = "standard"; action = "timeout"; duration = "60s"; }
            {
              route = "standard";
              action = "continue";
              idle = "30s";
              reshare = "full";
              retries = 2;
            }
            # --- standard.retry: full retryable universe, unused providers ---
            {
              route = "standard.retry";
              action = "filter";
              where = { error = { "in" = [ "404" "model_not_found" "429" "5xx" "timeout" "connection_error" "invalid_response" ]; }; };
            }
            { route = "standard.retry"; action = "filter"; where = { provider = { "in" = [ "gonka-proxy" "gonka-openbroker" ]; unused = true; }; }; }
            { route = "standard.retry"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
            { route = "standard.retry"; action = "rank"; strategy = "priority"; }
            { route = "standard.retry"; action = "race"; count = 1; }
            # --- standard.hedge: opt-in selection hedge over unused providers ---
            {
              route = "standard";
              action = "hedge";
              after = "20s";
              target = "standard.hedge";
            }
            { route = "standard.hedge"; action = "filter"; where = { provider = { "in" = [ "gonka-proxy" "gonka-openbroker" ]; unused = true; }; }; }
            { route = "standard.hedge"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
            { route = "standard.hedge"; action = "rank"; strategy = "priority"; }
            { route = "standard.hedge"; action = "race"; count = 1; }
            # --- smart (GLM): hyperfusion serves its own gonka/-prefixed native ---
            { route = "smart"; action = "filter"; where = { model = { eq = "smart"; }; }; }
            { route = "smart"; action = "filter"; where = { provider = { "in" = [ "gonka-proxy" ]; }; }; }
            { route = "smart"; action = "map"; native = "zai-org/GLM-5.3-Flash"; }
            { route = "smart"; action = "filter"; where = { provider = { "in" = [ "gonka-openbroker" ]; }; }; }
            { route = "smart"; action = "map"; native = "gonka/zai-org/GLM-5.3-Flash"; }
            { route = "smart"; action = "rank"; strategy = "priority"; }
            { route = "smart"; action = "balance"; strategy = "p2c"; }
            { route = "smart"; action = "race"; count = 0; }
            { route = "smart"; action = "retry"; target = "smart.retry"; attempts = 2; }
            { route = "smart"; action = "semaphore"; maxCalls = 6; maxInFlight = 4; maxCallsPerProvider = 1; }
            { route = "smart"; action = "timeout"; duration = "60s"; }
            # --- smart.fallback: stock universe-wide net over unused providers ---
            { route = "smart"; action = "fallback"; target = "smart.fallback"; }
            {
              route = "smart.fallback";
              action = "filter";
              where = { error = { "in" = [ "404" "model_not_found" "429" "5xx" "timeout" "connection_error" "invalid_response" ]; }; };
            }
            { route = "smart.fallback"; action = "filter"; where = { provider = { "in" = [ "gonka-proxy" "gonka-openbroker" ]; unused = true; }; }; }
            { route = "smart.fallback"; action = "map"; native = "zai-org/GLM-5.3-Flash"; }
            { route = "smart.fallback"; action = "rank"; strategy = "priority"; }
            { route = "smart.fallback"; action = "race"; count = 0; }
          ];
        };
      }
    ];
  }).config;

  cfgFile = config.lattice.llm-gateway.publicConfigFile;

  # Referencing a removed sugar option must fail fast during Nix evaluation.
  # `models` is a stale option; a config that still sets it should throw.
  staleSugarConfig = lib.nixosSystem {
    modules = [
      gatewayModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway.enable = true;
        lattice.llm-gateway.models.standard.native = "deepseek-ai/DeepSeek-V4-Flash-0731";
      }
    ];
  };
in
# The stale `models` option must not exist: evaluation throws with a clear
# "The option ... does not exist" error, so forcing it here is a fail-fast
# regression guard against resurrecting the sugar.
assert builtins.tryEval (staleSugarConfig.config.lattice.llm-gateway.models) == { success = false; value = false; };

pkgs.runCommand "llm-gateway-flat-evaluation" { nativeBuildInputs = [ pkgs.jq ]; }
  ''
    cfg=${cfgFile}

    # The generated config is a flat routing_rules array and nothing else
    # (no models/pipeline/plans residue).
    if jq -e 'has("models") or has("pipeline") or has("plans") or has("routes")' $cfg >/dev/null; then
      echo "routing contract must stay flat: no models/pipeline/plans/routes keys" >&2
      exit 1
    fi
    if ! jq -e '.routing_rules | type == "array"' $cfg >/dev/null; then
      echo "routing_rules must be a flat array" >&2
      exit 1
    fi

    # Every entry route is an explicit filter (where.model.eq): logical
    # models are derived from rules, never declared separately.
    for model in standard smart; do
      if ! jq -e --arg model "$model" 'any(.routing_rules[]; .action == "filter" and .where.model != null and .where.model.eq == $model)' $cfg >/dev/null; then
        echo "missing explicit entry filter for $model" >&2
        exit 1
      fi
    done

    # The continue takeover (in-gateway stream continuation) is declared
    # explicitly on the entry route carrying idle/reshare/retries.
    if ! jq -e 'any(.routing_rules[]; .route == "standard" and .action == "continue" and .idle == "30s" and .reshare == "full" and .retries == 2)' $cfg >/dev/null; then
      echo "expected explicit continue action on standard with idle/reshare/retries" >&2
      exit 1
    fi

    # The selection hedge is its own typed action targeting a hedge subroute.
    if ! jq -e 'any(.routing_rules[]; .route == "standard" and .action == "hedge" and .after == "20s" and .target == "standard.hedge")' $cfg >/dev/null; then
      echo "expected explicit hedge action on standard targeting standard.hedge" >&2
      exit 1
    fi
    if ! jq -e 'any(.routing_rules[]; .route == "standard.hedge" and .action == "map" and .native == "deepseek-ai/DeepSeek-V4-Flash-0731")' $cfg >/dev/null; then
      echo "expected hedge subroute mapping the entry native" >&2
      exit 1
    fi

    # smart maps two provider groups to two distinct natives (one per group),
    # both emitted as explicit filter→map pairs on the entry route.
    if ! jq -e 'any(.routing_rules[]; .route == "smart" and .action == "map" and .native == "zai-org/GLM-5.3-Flash")
       and any(.routing_rules[]; .route == "smart" and .action == "map" and .native == "gonka/zai-org/GLM-5.3-Flash")' $cfg >/dev/null; then
      echo "expected smart to map both the plain native and the gonka/-prefixed alias" >&2
      exit 1
    fi

    # Per-model semaphore/race overrides survive as explicit values on the
    # route (smart races the whole pool with an aggressive semaphore).
    if ! jq -e 'any(.routing_rules[]; .route == "smart" and .action == "race" and .count == 0)
       and any(.routing_rules[]; .route == "smart" and .action == "semaphore" and .max_calls == 6 and .max_in_flight == 4)' $cfg >/dev/null; then
      echo "expected smart race 0 with semaphore 6/4/1 explicit values" >&2
      exit 1
    fi

    # Transitions point at explicit fallback subroutes that own their filters.
    if ! jq -e 'any(.routing_rules[]; .route == "smart" and .action == "fallback" and .target == "smart.fallback")' $cfg >/dev/null; then
      echo "expected explicit fallback transition on smart" >&2
      exit 1
    fi
    if ! jq -e 'any(.routing_rules[]; .route == "smart.fallback" and .action == "filter" and .where.error["in"] != null)' $cfg >/dev/null; then
      echo "fallback subroute must own its error filter" >&2
      exit 1
    fi

    touch $out
  ''
