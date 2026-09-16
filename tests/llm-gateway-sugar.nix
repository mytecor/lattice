{ nixpkgs, pkgs, gatewayModule }:

let
  inherit (nixpkgs) lib;

  # The f7-14 sugar contract: `models` + `pipeline` generate the canonical
  # bounded pipeline (entry filter → provider filter → map → rank → balance →
  # affinity → race → retry → [hedge] → semaphore → timeout plus the
  # <model>.retry / <model>.hedge subroutes) into the same flat routing_rules
  # array raw rules use; raw rules remain the escape hatch for fallbacks.
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
            };
            openbroker = {
              id = "gonka-openbroker";
              inferenceUrl = "https://openbroker.gonka.invalid/v1";
              apiKeyFile = "/run/agenix/llm-provider-openbroker";
              priority = 10;
            };
          };
          pipeline = {
            providers = [ "gonka-proxy" "gonka-openbroker" ];
            balance.strategy = "p2c";
            retry.attempts = 2;
          };
          models = {
            standard.native = "deepseek-ai/DeepSeek-V4-Flash-0731";
            # smart narrows nothing (inherits the deployment providers list
            # through the override chain) but races the whole pool and opts
            # into the hedge with a custom delay.
            smart = {
              native = "zai-org/GLM-5.3-Flash";
              pipeline = {
                raceCount = 0;
                hedge = {
                  enable = true;
                  after = "2s";
                };
              };
            };
          };
          # Escape hatch: a raw fallback extending the generated entry route.
          routingRules = [
            { route = "standard"; action = "fallback"; target = "standard.fallback"; }
            {
              route = "standard.fallback";
              action = "filter";
              where = { error = { "in" = [ "429" "5xx" ]; }; };
            }
            {
              route = "standard.fallback";
              action = "filter";
              where = { provider = { "in" = [ "gonka-proxy" ]; }; };
            }
            { route = "standard.fallback"; action = "map"; native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731"; }
            { route = "standard.fallback"; action = "rank"; strategy = "priority"; }
            { route = "standard.fallback"; action = "race"; count = 1; }
          ];
        };
      }
    ];
  }).config;

  # A raw rule re-filtering an entry model must trip the duplicate-entry
  # assertion (the sugar already generates that entry route).
  duplicateEntryConfig = (lib.nixosSystem {
    modules = [
      gatewayModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway.enable = true;
        lattice.llm-gateway = {
          providers.proxy = {
            id = "gonka-proxy";
            inferenceUrl = "https://proxy.gonka.invalid/v1";
            apiKeyFile = "/run/agenix/llm-provider-proxy";
          };
          models.standard.native = "deepseek-ai/DeepSeek-V4-Flash-0731";
          routingRules = [
            { route = "standard"; action = "filter"; where = { model = { eq = "standard"; }; }; }
          ];
        };
      }
    ];
  }).config;

  # A raw rule on a generated subroute name is forbidden: those routes are
  # owned by the sugar.
  subrouteCollisionConfig = (lib.nixosSystem {
    modules = [
      gatewayModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway.enable = true;
        lattice.llm-gateway = {
          providers.proxy = {
            id = "gonka-proxy";
            inferenceUrl = "https://proxy.gonka.invalid/v1";
            apiKeyFile = "/run/agenix/llm-provider-proxy";
          };
          models = {
            standard.native = "deepseek-ai/DeepSeek-V4-Flash-0731";
            smart = {
              native = "zai-org/GLM-5.3-Flash";
              pipeline.hedge.enable = true;
            };
          };
          routingRules = [
            {
              route = "smart.hedge";
              action = "filter";
              where = { provider = { "in" = [ "gonka-proxy" ]; }; };
            }
          ];
        };
      }
    ];
  }).config;

  # A dot in a model name collides with the <model>.retry / <model>.hedge
  # subroute naming convention.
  dottedModelConfig = (lib.nixosSystem {
    modules = [
      gatewayModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.llm-gateway.enable = true;
        lattice.llm-gateway = {
          providers.proxy = {
            id = "gonka-proxy";
            inferenceUrl = "https://proxy.gonka.invalid/v1";
            apiKeyFile = "/run/agenix/llm-provider-proxy";
          };
          models."bad.model".native = "native/id";
        };
      }
    ];
  }).config;

  failedAssertions = cfg:
    lib.filter (assertion: !assertion.assertion) cfg.assertions;
  # Only the llm-gateway module's own contract assertions: the minimal test
  # system also trips base NixOS assertions (no root file system), which are
  # irrelevant here. Every llm-gateway assertion message carries the module
  # prefix so it can be selected from the toplevel assertion list.
  gatewayFailures = cfg:
    lib.filter
      (assertion: lib.hasPrefix "llm-gateway: " assertion.message)
      (failedAssertions cfg);
in
assert gatewayFailures config == [ ];
assert lib.length (gatewayFailures duplicateEntryConfig) == 1;
assert lib.hasInfix
  "must not re-filter a model declared in models"
  (lib.head (gatewayFailures duplicateEntryConfig)).message;
assert lib.length (gatewayFailures subrouteCollisionConfig) == 1;
assert lib.hasInfix
  "generated subroute names"
  (lib.head (gatewayFailures subrouteCollisionConfig)).message;
assert lib.length (gatewayFailures dottedModelConfig) == 1;
assert lib.hasInfix
  "dot-free"
  (lib.head (gatewayFailures dottedModelConfig)).message;
pkgs.runCommand "llm-gateway-sugar-evaluation" { nativeBuildInputs = [ pkgs.jq ]; }
  ''
    cfg=${config.lattice.llm-gateway.publicConfigFile}

    # Balance strategy default of the generated pipeline is p2c.
    grep -q '"action":"balance"' $cfg
    if ! jq -e 'any(.routing_rules[]; .action == "balance" and .strategy == "p2c")' $cfg >/dev/null; then
      echo "generated balance rules must use strategy p2c" >&2
      exit 1
    fi
    # Equal weights by default: the generated balance object carries no
    # non-default weights.
    if jq -e 'any(.routing_rules[]; .action == "balance" and (.weights | length > 0))' $cfg >/dev/null; then
      echo "generated balance rules must not carry weights by default" >&2
      exit 1
    fi

    # The deployment pipeline providers list narrows the entry filter.
    if ! jq -e 'any(.routing_rules[]; .action == "filter" and .where.provider["in"] != null and (.where.provider["in"] | sort) == ["gonka-openbroker","gonka-proxy"])' $cfg >/dev/null; then
      echo "provider filter must carry the explicit pipeline provider list" >&2
      exit 1
    fi

    # Entry routes: one per declared model, generated (model filter present).
    for model in standard smart; do
      if ! jq -e --arg model "$model" 'any(.routing_rules[]; .action == "filter" and .where.model.eq == $model)' $cfg >/dev/null; then
        echo "missing generated entry filter for $model" >&2
        exit 1
      fi
    done

    # Retry subroutes exist for every model and race one target.
    if ! jq -e '([.routing_rules[] | select(.route | endswith(".retry"))] | length) >= 2' $cfg >/dev/null; then
      echo "expected <model>.retry subroutes for every model" >&2
      exit 1
    fi
    if ! jq -e 'any(.routing_rules[]; .action == "retry" and .target == "standard.retry" and .attempts == 2)' $cfg >/dev/null; then
      echo "expected retry transition to standard.retry with 2 attempts" >&2
      exit 1
    fi

    # Hedge is opt-in: smart opted in (with a custom delay), standard did not.
    if ! jq -e 'any(.routing_rules[]; .action == "hedge" and .route == "smart" and .after == "2s" and .target == "smart.hedge")' $cfg >/dev/null; then
      echo "expected opt-in hedge on smart with after=2s" >&2
      exit 1
    fi
    if ! jq -e 'any(.routing_rules[]; .route == "smart.hedge" and .action == "filter" and .where.provider.unused == true)' $cfg >/dev/null; then
      echo "expected hedge subroute re-selecting unused providers" >&2
      exit 1
    fi
    if jq -e 'any(.routing_rules[]; .route == "standard.hedge")' $cfg >/dev/null; then
      echo "hedge must not be generated when disabled (standard)" >&2
      exit 1
    fi

    # Per-model raceCount override: smart races the whole pool (0), standard
    # keeps the deployment default 1.
    if ! jq -e 'any(.routing_rules[]; .action == "race" and .route == "smart" and .count == 0)' $cfg >/dev/null; then
      echo "expected smart race count 0 override" >&2
      exit 1
    fi
    if ! jq -e 'any(.routing_rules[]; .action == "race" and .route == "standard" and .count == 1)' $cfg >/dev/null; then
      echo "expected standard race count 1 from the deployment pipeline" >&2
      exit 1
    fi

    # The raw fallback rule extends the generated entry route and keeps its
    # own subroute; no duplicate entry filter for the same logical model.
    if ! jq -e 'any(.routing_rules[]; .action == "fallback" and .route == "standard" and .target == "standard.fallback")' $cfg >/dev/null; then
      echo "expected raw fallback action on the generated standard route" >&2
      exit 1
    fi
    if [ "$(jq '[.routing_rules[] | select(.action == "filter" and .where.model != null and .where.model.eq == "standard")] | length' $cfg)" -ne 1 ]; then
      echo "entry model standard must be filtered exactly once" >&2
      exit 1
    fi
    touch $out
  ''
