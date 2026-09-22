# Shared typed machinery of the llm-gateway module. Plain function (not a
# module) so both module files — options.nix (option declarations) and
# config.nix (public config generation from the `models`/`pipeline` sugar) —
# share exactly one definition of the discriminated rule submodules, the
# `rewriteRule` evaluator and the optional pipeline shape.
{ lib }:

let
  inherit (lib) mkOption types;

  # Upper bound of the continue rule's whole-chain retry budget; mirrors
  # maxContinueChainRetries in packages/llm-gateway/config.go.
  maxContinueChainRetries = 10;

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
    balance = { config, ... }: {
      options = {
        strategy = mkOption {
          type = types.enum [ "p2c" "round_robin" "adaptive" "weighted" ];
          default = "p2c";
          description = ''
            Runtime provider-selection strategy of the balance action. p2c
            (power of two choices) draws two random healthy candidates and
            promotes the one with fewer in-flight branches — the default, it
            distributes under concurrency without latency feedback;
            round_robin rotates equally over the healthy candidates, adaptive
            weights by static weight × health, weighted uses only the static
            weights.
          '';
        };
        weights = mkOption {
          type = types.attrsOf (types.ints.positive);
          default = { };
          description = ''
            Static per-provider weights; the default is an equal weight (1)
            for every provider — provider priority never feeds the runtime
            choice. round_robin and p2c ignore them (p2c uses them only to
            bias the first random draw, which is uniform for equal weights).
          '';
        };
        window = mkOption {
          type = types.strMatching "[0-9]+(ms|s|m|h)";
          default = "5m";
          description = "Health window; the error budget is evaluated over this sliding window.";
        };
        errorBudget = mkOption {
          type = types.float;
          default = 0.2;
          description = "Max share of health errors (429, 5xx, timeout, connection_error) inside which a provider stays healthy; a provider at or above the budget is excluded from the p2c / adaptive / round-robin choice.";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = {
            strategy = config.strategy;
            weights = config.weights;
            window = config.window;
            error_budget = config.errorBudget;
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
    # continue: in-gateway stream takeover policy on an entry route. When the
    # relayed winner stream stalls past `idle` or closes without a
    # finish_reason after producing meaningful content/reasoning, the gateway
    # continues the same client stream by re-dispatching the request (with the
    # partial output reshaped per `reshare`) to a different provider, instead
    # of surfacing an error to the client. `retries` bounds whole-chain
    # re-dispatches: when every provider in the pool has broken during the
    # request, the gateway re-dispatches the whole chain from the top with the
    # reshared partial while the budget lasts, instead of surfacing a terminal
    # error. Only affects streaming chat.
    continue = { config, ... }: {
      options = {
        idle = mkOption {
          type = types.strMatching "[0-9]+(ms|s|m|h)";
          description = "Stall threshold for the relayed winner stream at which the takeover triggers; replaces the route-wide stream idle timeout while the policy is active.";
        };
        reshare = mkOption {
          type = types.enum [ "full" ];
          default = "full";
          description = "How partial output is handed to the next provider: `full` re-shapes every relayed reasoning/content delta as assistant context appended to the request history.";
        };
        retries = mkOption {
          type = types.ints.between 0 maxContinueChainRetries;
          default = 0;
          description = "Whole-chain retry budget: how many times an exhausted chain (every provider broke during the request) is re-dispatched from the top with the reshared partial. 0 disables (a terminal error is surfaced once the chain runs out). Bounded by ${toString maxContinueChainRetries} to keep the worst-case per-request work finite.";
        };
        wait = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Bounded horizon the gateway holds the relayed stream open while the provider pool recovers from an exhausted takeover, instead of surfacing a terminal error the client would see. During the wait the gateway sends SSE keep-alives and re-attempts the continuation until a provider recovers, the client disconnects, or the horizon expires (then the terminal error surfaces as a last resort). null/\"0\" keep the wait enabled with the gateway's built-in default horizon; an explicit duration sets the horizon (e.g. \"10m\").";
        };
        _public = mkOption {
          type = types.attrs;
          internal = true;
          readOnly = true;
          default = { inherit (config) idle reshare retries; } // lib.optionalAttrs (config.wait != null) { wait = config.wait; };
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

  # Optional pipeline shape shared by the deployment-level `pipeline` defaults
  # and the per-model `models.<name>.pipeline` overrides. Every field is
  # nullable; the generator in config.nix coalesces per-model override →
  # deployment default → built-in default, so this module never invents
  # defaults of its own.
  pipelineSubmodule = { config, ... }: {
    options = {
      # null = every enabled provider; an explicit list narrows the universe.
      providers = mkOption {
        type = types.nullOr (types.nonEmptyListOf types.str);
        default = null;
        description = ''
          Provider IDs available to the generated pipeline (entry and
          retry/hedge subroutes); null selects every enabled provider.
        '';
      };
      raceCount = mkOption {
        type = types.nullOr types.ints.unsigned;
        default = null;
        description = "Race batch size of the entry route (0 = the whole pool).";
      };
      balance = {
        strategy = mkOption {
          type = types.nullOr (types.enum [ "p2c" "round_robin" "adaptive" "weighted" ]);
          default = null;
          description = "Balance strategy; null resolves to p2c.";
        };
        weights = mkOption {
          type = types.nullOr (types.attrsOf types.ints.positive);
          default = null;
          description = "Static per-provider weights; null leaves every provider at equal weight (1).";
        };
        window = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Health window; null leaves the gateway default (5m).";
        };
        errorBudget = mkOption {
          type = types.nullOr types.float;
          default = null;
          description = "Max share of health errors inside which a provider stays healthy; null leaves the gateway default (0.2).";
        };
      };
      retry = {
        attempts = mkOption {
          type = types.nullOr types.ints.positive;
          default = null;
          description = "Retry attempts; null leaves the built-in default (2).";
        };
        backoffType = mkOption {
          type = types.nullOr (types.enum [ "constant" "exponential" ]);
          default = null;
          description = "Retry backoff shape; null leaves the gateway default (exponential).";
        };
        backoffInitial = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Retry backoff initial delay; null leaves the sugar default (200ms; a handwritten retry rule without the field defaults to 100ms).";
        };
        backoffMax = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Retry backoff ceiling; null leaves the gateway default (1s).";
        };
      };
      hedge = {
        enable = mkOption {
          type = types.nullOr types.bool;
          default = null;
          description = "Hedge is opt-in: null/false generates no hedge action.";
        };
        after = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Hedge delay; null leaves the gateway default (3s).";
        };
      };
      semaphore = {
        maxCalls = mkOption {
          type = types.nullOr types.ints.positive;
          default = null;
          description = "Total upstream calls per client request; null leaves the built-in default (4).";
        };
        maxInFlight = mkOption {
          type = types.nullOr types.ints.positive;
          default = null;
          description = "Simultaneously executing upstream calls per client request; null leaves the built-in default (3).";
        };
        maxCallsPerProvider = mkOption {
          type = types.nullOr types.ints.positive;
          default = null;
          description = "Upstream calls to one provider per client request; null leaves the built-in default (1).";
        };
      };
      timeout = {
        duration = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Route timeout; null leaves the built-in default (60s).";
        };
      };
      affinityTtl = mkOption {
        type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
        default = null;
        description = "Affinity mapping TTL; null leaves the built-in default (24h).";
      };
      continue = {
        enable = mkOption {
          type = types.nullOr types.bool;
          default = null;
          description = ''
            In-gateway stream takeover for the generated entry route: when the
            relayed winner stream stalls past `idle` or closes without a
            finish_reason, the gateway continues the same client stream by
            re-dispatching the request (with the partial output reshared) to a
            different provider. null/false generates no continue action.
          '';
        };
        idle = mkOption {
          type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
          default = null;
          description = "Stall threshold (silence after the last event) that trips the takeover; null leaves the sugar default (90s). Must be >= 5s.";
        };
        reshare = mkOption {
          type = types.nullOr (types.enum [ "full" ]);
          default = null;
          description = "How partial output is handed to the next provider; null leaves the sugar default (full).";
        };
        retries = mkOption {
          type = types.nullOr (types.ints.between 0 maxContinueChainRetries);
          default = null;
          description = "Whole-chain retry budget: how many times an exhausted chain is re-dispatched from the top with the reshared partial; null leaves the sugar default (0, disabled — a terminal error is surfaced once the chain runs out). Bounded by ${toString maxContinueChainRetries}.";
        };
      };
    };
  };
in
{
  inherit routingRuleType rewriteRule pipelineSubmodule;
}
