{ lib, pkgs, ... }:

let
  inherit (lib) mkOption types;

  # Supported typed failure classes, shared by all error filters.
  errorClasses = [ "timeout" "connection_error" "429" "404" "invalid_response" "model_not_found" "5xx" ];

  # `where` submodules for the filter action: each declares exactly one
  # condition dimension, typed with the module system, plus an internal
  # `_public` projection emitting the snake_case JSON.
  whereSubmodules = {
    model = { config, ... }: {
      options = {
        eq = mkOption {
          type = types.str;
          description = "Logical model this entry route serves.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { eq = config.eq; };
        };
      };
    };
    provider = { config, ... }: {
      options = {
        "in" = mkOption {
          type = types.nullOr (types.nonEmptyListOf types.str);
          default = null;
          description = "Optional non-empty provider IDs selected from the route provider universe.";
        };
        notIn = mkOption {
          type = types.listOf types.str;
          default = [ ];
          description = "Provider IDs excluded from the selection.";
        };
        unused = mkOption {
          type = types.bool;
          default = false;
          description = "Restrict the pool to providers not yet used by the current request graph.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = lib.optionalAttrs (config."in" != null) {
            "in" = config."in";
          } // lib.optionalAttrs (config.notIn != [ ]) {
            not_in = config.notIn;
          } // lib.optionalAttrs config.unused {
            unused = true;
          };
        };
      };
    };
    error = { config, ... }: {
      options = {
        "in" = mkOption {
          type = types.listOf (types.enum errorClasses);
          description = "Failure classes that admit a transition into this route.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { "in" = config."in"; };
        };
      };
    };
    attempt = { config, ... }: {
      options = {
        lt = mkOption {
          type = types.ints.positive;
          description = "Route applies while the current attempt number is below this bound.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { lt = config.lt; };
        };
      };
    };
  };

  # filterWhereType evaluates exactly one `where` dimension and returns its
  # `_public` projection; an unknown dimension or more than one dimension
  # throws during Nix evaluation.
  filterWhereType = types.mkOptionType {
    name = "filterWhere";
    description = "single-dimension where condition of a filter action";
    check = value: builtins.isAttrs value
      && builtins.length (builtins.attrNames value) == 1
      && builtins.hasAttr (builtins.head (builtins.attrNames value)) whereSubmodules;
    merge = _loc: defs:
      let
        combined = builtins.foldl' (acc: def: acc // def.value) { } defs;
        dims = builtins.attrNames combined;
      in
      if builtins.length dims != 1 then
        throw "filter where must declare exactly one of model, provider, error, attempt"
      else
        let dim = builtins.head dims; in
        {
          ${dim} = (lib.evalModules {
            modules = [
              whereSubmodules.${dim}
              { config = combined.${dim}; }
            ];
          }).config._public;
        };
    emptyValue = { };
  };

  # Per-action submodules: each declares exactly the options its action owns,
  # typed with the module system, plus an internal `_public` projection that
  # picks exactly those fields for the generated public JSON. Opening the
  # module system on the action's submodule gives full per-action typing and
  # rejects unknown or foreign fields during Nix evaluation; the gateway
  # binary independently re-validates the generated JSON at startup.
  actionSubmodules = {
    filter = { config, ... }: {
      options = {
        where = mkOption {
          type = filterWhereType;
          description = ''
            Single-dimension condition of the filter action: model (entry
            applicability), provider (provider selection), error (transition
            applicability) or attempt (entry bound).
          '';
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          description = "Action-owned fields emitted into the public JSON.";
          default = { where = config.where; };
        };
      };
    };
    map = { config, ... }: {
      options = {
        native = mkOption {
          type = types.str;
          description = "Provider-native model id bound to the current provider selection.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          description = "Action-owned fields emitted into the public JSON.";
          default = { native = config.native; };
        };
      };
    };
    rank = { config, ... }: {
      options = {
        strategy = mkOption {
          type = types.enum [ "priority" ];
          default = "priority";
          description = "Candidate pool ranking strategy.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { strategy = config.strategy; };
        };
      };
    };
    lease = { config, ... }: {
      options = {
        source = mkOption {
          type = types.enum [ "winner" ];
          default = "winner";
          description = "Lease source; only winner is implemented.";
        };
        duration = mkOption {
          type = types.strMatching "[0-9]+(ms|s|m|h)";
          description = "Winner lease duration; required and must be positive.";
        };
        renewOnSuccess = mkOption {
          type = types.bool;
          default = true;
          description = "Renew the winner lease on every successful response.";
        };
        releaseOn = mkOption {
          type = types.listOf (types.enum errorClasses);
          default = [ ];
          description = "Hard failure classes that release the winner lease.";
        };
        releaseAfterSlowStarts = mkOption {
          type = types.int;
          default = 0;
          description = "Release the lease after this many consecutive slow starts of the holder.";
        };
        slowStart = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Meaningful TTFT threshold classifying a start as slow.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = {
            source = config.source;
            duration = config.duration;
            renew_on_success = config.renewOnSuccess;
            release_on = config.releaseOn;
            release_after_slow_starts = config.releaseAfterSlowStarts;
            slow_start = config.slowStart;
          };
        };
      };
    };
    affinity = { config, ... }: {
      options = {
        sources = mkOption {
          type = types.listOf (types.enum [ "responses.conversation" "responses.previous_response_id" ]);
          description = "Responses protocol identifiers used to pin a stateful route.";
        };
        ttl = mkOption {
          type = types.strMatching "[0-9]+(ms|s|m|h)";
          description = "Affinity mapping TTL; required and must be positive.";
        };
        onMissing = mkOption {
          type = types.enum [ "ignore" ];
          default = "ignore";
          description = "Behavior for unknown or missing affinity identifiers.";
        };
        onProviderFailure = mkOption {
          type = types.enum [ "fail-closed" ];
          default = "fail-closed";
          description = "Behavior when the pinned affinity provider fails.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = {
            sources = config.sources;
            ttl = config.ttl;
            on_missing = config.onMissing;
            on_provider_failure = config.onProviderFailure;
          };
        };
      };
    };
    # race: route-creating action; keeps an immutable pool snapshot.
    race = { config, ... }: {
      options = {
        count = mkOption {
          type = types.ints.unsigned;
          default = 0;
          description = "Race batch size (0 = the whole pool).";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { count = config.count; };
        };
      };
    };
    retry = { config, ... }: {
      options = {
        target = mkOption {
          type = types.str;
          description = "Named subroute executed on a retryable failure; its own filter decides applicability.";
        };
        attempts = mkOption {
          type = types.ints.positive;
          description = "Retry attempts; required and must be at least 1.";
        };
        backoffType = mkOption { type = types.enum [ "constant" "exponential" ]; default = "exponential"; };
        backoffInitial = mkOption { type = types.strMatching "[0-9]+(ms|s|m|h)"; default = "100ms"; };
        backoffMax = mkOption { type = types.strMatching "[0-9]+(ms|s|m|h)"; default = "1s"; };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = {
            target = config.target;
            attempts = config.attempts;
            backoff = {
              type = config.backoffType;
              initial = config.backoffInitial;
              max = config.backoffMax;
            };
          };
        };
      };
    };
    hedge = { config, ... }: {
      options = {
        after = mkOption {
          type = types.strMatching "[0-9]+(ms|s|m|h)";
          description = "Delay after which the target route may start while the current branches are still running; required and must be positive.";
        };
        target = mkOption {
          type = types.str;
          description = "Named subroute raced as the hedge target.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { after = config.after; target = config.target; };
        };
      };
    };
    semaphore = { config, ... }: {
      options = {
        maxCalls = mkOption {
          type = types.int;
          description = "Total upstream calls allowed per client request; required and must be positive.";
        };
        maxInFlight = mkOption {
          type = types.int;
          description = "Simultaneously executing upstream calls per client request; required and must be positive.";
        };
        maxCallsPerProvider = mkOption {
          type = types.int;
          description = "Upstream calls to one provider inside a client request; required and must be positive.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = {
            max_calls = config.maxCalls;
            max_in_flight = config.maxInFlight;
            max_calls_per_provider = config.maxCallsPerProvider;
          };
        };
      };
    };
    timeout = { config, ... }: {
      options = {
        duration = mkOption {
          type = types.strMatching "[0-9]+(ms|s|m|h)";
          description = "Route timeout bounding the whole route graph; required and must be positive.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { duration = config.duration; };
        };
      };
    };
    # fallback: one-shot explicit transition to a named subroute, compiled by
    # the same named-route compiler as every other route.
    fallback = { config, ... }: {
      options = {
        target = mkOption {
          type = types.str;
          description = "Named subroute executed on the current route's failure; its own filter decides applicability.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { target = config.target; };
        };
      };
    };
  };

  # rewriteRule validates one raw rule (route + action + the action's own
  # fields) and evaluates its action-specific submodule so the merged value is
  # fully typed and carries the `_public` JSON projection. Unknown actions,
  # unknown fields and fields owned by another action throw during Nix
  # evaluation; the gateway binary re-validates the generated JSON at startup
  # for anything produced outside Nix.
  rewriteRule = raw:
    let
      route = raw.route or null;
      action = raw.action or null;
    in
    if ! (builtins.isString route) then
      throw "routing rule ${builtins.toJSON action} has no string route"
    else if ! (builtins.isString action) then
      throw "routing rule for route \"${route}\" has no action name"
    else if ! (builtins.hasAttr action actionSubmodules) then
      throw "routing rule for route \"${route}\" has unsupported action \"${action}\""
    else
      let
        evaluated = lib.evalModules {
          modules = [
            actionSubmodules.${action}
            { config = builtins.removeAttrs raw [ "route" "action" ]; }
          ];
        };
      in
      (builtins.removeAttrs evaluated.config [ "_module" ]) // { inherit route action; };

  routingRuleType = types.mkOptionType {
    name = "routingRule";
    description = "discriminated routing rule carrying only its own action's fields";
    check = value: builtins.isAttrs value && value ? route && value ? action;
    merge = loc: defs:
      rewriteRule (builtins.foldl' (acc: def: acc // def.value) { } defs);
    emptyValue = { };
  };
in
{
  options.lattice.llm-gateway = {
    enable = mkOption {
      type = types.bool;
      default = false;
      description = "Enable the Lattice OpenAI-compatible LLM gateway.";
    };

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.llm-gateway;
      defaultText = lib.literalExpression "pkgs.lattice.llm-gateway";
      description = "Package providing the gateway binary.";
    };

    user = mkOption {
      type = types.str;
      default = "llm-gateway";
      description = "Unprivileged user that runs the gateway.";
    };

    group = mkOption {
      type = types.str;
      default = "llm-gateway";
      description = "Group that runs the gateway.";
    };

    runtimeDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "llm-gateway";
      description = "systemd RuntimeDirectory name below /run.";
    };

    host = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = "Gateway listen address.";
    };

    port = mkOption {
      type = types.port;
      default = 9208;
      description = "Gateway listen port.";
    };

    clientCredentialFile = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Runtime path to the gateway client key, normally an agenix secret. The file is loaded
        with systemd LoadCredential and is never read during Nix evaluation.
      '';
    };

    logLevel = mkOption {
      type = types.enum [ "silent" "error" "warn" "info" "debug" "trace" ];
      default = "silent";
      description = "Gateway log level. Silent is the secure production default.";
    };

    catalogRefreshInterval = mkOption {
      type = types.strMatching "[0-9]+(ms|s|m|h)";
      default = "10m";
      description = "Refresh interval for internal provider model catalogs.";
    };

    providers = mkOption {
      default = { };
      description = "Bifrost-backed provider instances for the Lattice-owned proxy.";
      type = types.attrsOf (types.submodule ({ name, ... }: {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = "Include this provider instance in runtime configuration.";
          };
          id = mkOption {
            type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
            default = name;
            description = "Stable Bifrost custom-provider instance ID.";
          };
          baseProvider = mkOption {
            type = types.enum [ "openai" "anthropic" "cohere" "gemini" "huggingface" "replicate" ];
            default = "openai";
            description = "Bifrost base provider adapter used by this custom provider instance.";
          };
          inferenceUrl = mkOption {
            type = types.str;
            description = ''
              Provider inference base URL. For baseProvider `openai`, this is the
              full OpenAI-compatible base path including the version segment: the
              gateway appends only the operation and never inserts `/v1`.
            '';
          };
          modelsUrl = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = ''
              Optional independent model catalog URL. For baseProvider `openai`,
              the gateway derives it by appending `/models` to inferenceUrl when
              unset. Other adapters require an explicit URL for discovery.
            '';
          };
          apiKeyFile = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "Runtime path to the provider API key loaded with systemd credentials.";
          };
          modelsApiKeyFile = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = ''
              Optional separate credential for an explicit modelsUrl. An inferred
              same-provider catalog uses apiKeyFile.
            '';
          };
          priority = mkOption {
            type = types.int;
            default = 0;
            description = "Higher-priority providers are launched first in serial and hedged stages.";
          };
          cooldown = mkOption {
            type = types.strMatching "[0-9]+(ms|s|m|h)";
            default = "15s";
            description = "Circuit-breaker cooldown after retryable failures.";
          };
          requestTimeout = mkOption {
            type = types.strMatching "[0-9]+(ms|s|m|h)";
            default = "60s";
            description = "Bifrost provider request timeout.";
          };
          bifrostMaxRetries = mkOption {
            type = types.ints.unsigned;
            default = 0;
            description = "Provider-internal Bifrost retries; routing_rules retries wrap the composed route.";
          };
          allowPrivateNetwork = mkOption {
            type = types.bool;
            default = false;
            description = "Allow Bifrost to call private addresses; intended for controlled test/LAN providers.";
          };
          headers = mkOption {
            type = types.attrsOf types.str;
            default = { };
            description = "Non-secret extra headers sent to the provider.";
          };
        };
      }));
    };

    routingRules = mkOption {
      default = [ ];
      description = ''
        Flat ordered routing table for logical models. Every rule belongs to a
        named route (the route scope; dots such as "standard.retry" are a
        naming convention only). The physical config stays a flat array: there
        is no nested routes/plans structure.

        Per route, the typical order is
        filter (model / provider / error / attempt) → map (native mapping) →
        rank → lease → affinity → race → retry/hedge (target) →
        semaphore → timeout, and every transition (retry, fallback, hedge)
        points at its own named subroute with «target». Retry/fallback/hedge
        own no applicability: the destination route's own filter decides it.

        Each entry is a discriminated rule for exactly one action: it owns only
        that action's fields, and an unknown action, an unknown field or a
        field owned by another action fails during Nix evaluation. The gateway
        binary independently re-validates the generated JSON at startup, so
        JSON produced outside Nix receives the same strict per-action checks.
      '';
      type = types.listOf routingRuleType;
    };

    affinityFile = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Runtime path for the opaque Responses affinity mapping file. The file is
        owned by the gateway user with mode 0600 and contains only opaque id →
        provider mappings with expiry; never prompts, keys, or provider URLs. It
        survives service restarts in the runtime directory and is cleared on a
        full service stop or reboot.
      '';
    };

    publicConfigFile = mkOption {
      type = types.path;
      readOnly = true;
      description = "Generated non-secret gateway configuration template in the Nix store.";
    };
  };
}
