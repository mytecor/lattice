{ lib, pkgs, ... }:

let
  inherit (lib) mkOption types;

  shared = import ./types.nix { inherit lib; };
  inherit (shared) routingRuleType pipelineSubmodule;
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

    metricsHost = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = "Metrics listen address. Loopback by default so the Prometheus scrape endpoint is not public; wire it to a private scrape network explicitly.";
    };

    metricsPort = mkOption {
      type = types.port;
      default = 9209;
      description = "Metrics listen port (Prometheus text exposition). Must not equal the API port.";
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

    streamIdleTimeout = mkOption {
      type = types.strMatching "[0-9]+(ms|s|m|h)";
      default = "5m";
      description = ''
        Idle timeout for the relayed winner stream: a stream producing no
        events at all for this long is cancelled and reported to the client as
        a structured timeout error, and the failure is recorded against the
        provider (cooldown, health, lease). Any event, including provider
        keep-alives, re-arms the timer. The 5m default is deliberately
        conservative because reasoning models may legitimately pause
        mid-stream.
      '';
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
            description = ''
              Compile-time pool order: higher-priority providers are ranked first
              (the fail-open order and the order race/hedge batches are built
              in). Priority never feeds the runtime balance choice.
            '';
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

    pipeline = mkOption {
      default = { };
      description = ''
        Deployment-level defaults for the generated per-model routing
        pipelines (f7-14). Every field is optional; a field left unset
        resolves to the built-in default (providers = all enabled, balance
        strategy p2c with equal weights, race count 1, retry attempts 2 with
        exponential backoff, no hedge, semaphore 4/3/1, timeout 60s, affinity
        TTL 24h). Per-model overrides live in
        [models](#opt-lattice.llm-gateway.models)._pipeline.
      '';
      type = types.submodule pipelineSubmodule;
    };

    models = mkOption {
      default = { };
      description = ''
        Logical models served by generated routing pipelines (f7-14). Each
        entry generates the canonical bounded pipeline for one logical model:
        entry filter (where.model) → provider filter → map (native) → rank →
        balance (p2c by default) → affinity → race → retry (+ optional opt-in
        hedge) → semaphore → timeout, plus the `<model>.retry` and — when
        hedge is enabled — `<model>.hedge` named subroutes. The generated
        rules land in the same flat routing_rules array as
        [routingRules](#opt-lattice.llm-gateway.routingRules) (escape hatch
        for anything the sugar cannot express, e.g. native-alias fallbacks).
      '';
      type = types.attrsOf (types.submodule ({ name, ... }: {
        options = {
          native = mkOption {
            type = types.nonEmptyStr;
            description = "Provider-native model id mapped for this logical model.";
          };
          nativeByProvider = mkOption {
            type = types.attrsOf types.nonEmptyStr;
            default = { };
            description = ''
              Per-provider native model overrides for this logical model. A
              provider listed here is mapped to the given native ID instead of
              `native`; the sugar emits one filter (provider in group) + map
              pair per distinct native, so different providers of one logical
              model can reach it through different native IDs (f7-10) without
              a fallback. Keys must be enabled provider IDs present in the
              model's effective provider list.
            '';
          };
          pipeline = mkOption {
            default = { };
            description = ''
              Per-model pipeline overrides merged over the deployment-level
              [pipeline](#opt-lattice.llm-gateway.pipeline) defaults.
            '';
            type = types.submodule pipelineSubmodule;
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

        Rules generated from [models](#opt-lattice.llm-gateway.models) are
        prepended to this list; raw rules may extend a generated entry route
        (typically a `fallback` action pointing at a raw subroute), but a raw
        rule must never re-filter an entry model — that would create a second
        entry route for the same logical model.
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
