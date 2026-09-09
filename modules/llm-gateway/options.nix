{ lib, pkgs, ... }:

let
  inherit (lib) mkOption types;
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
        Flat ordered routing pipeline for logical models. The canonical order is
        map → rank → lease → affinity → race → retry → hedge → semaphore → timeout,
        optionally followed by the fallback stage map(s) → fallback. map binds
        one native model id to a set of provider IDs; each action accepts only
        its own fields and the gateway binary rejects unknown or misplaced
        fields at startup.
      '';
      type = types.listOf (types.submodule {
        options = {
          model = mkOption { type = types.str; };
          action = mkOption {
            type = types.enum [
              "map" "rank" "lease" "affinity"
              "race" "retry" "hedge" "semaphore" "timeout"
              "fallback"
            ];
          };
          # map
          native = mkOption {
            type = types.str;
            description = "Provider-native model id bound to the listed providers.";
          };
          providers = mkOption {
            type = types.listOf types.str;
            description = "Provider IDs receiving this native model in this stage.";
          };
          # rank
          strategy = mkOption {
            type = types.enum [ "priority" ];
            default = "priority";
            description = "Candidate pool ranking strategy.";
          };
          # lease
          source = mkOption {
            type = types.enum [ "winner" ];
            default = "winner";
            description = "Lease source; only winner is implemented.";
          };
          duration = mkOption {
            type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
            default = null;
            description = "Lease duration or route timeout.";
          };
          renewOnSuccess = mkOption {
            type = types.bool;
            default = true;
            description = "Renew the winner lease on every successful response.";
          };
          releaseOn = mkOption {
            type = types.listOf (types.enum [ "timeout" "connection_error" "429" "5xx" "404" "invalid_response" ]);
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
          # affinity
          sources = mkOption {
            type = types.listOf (types.enum [ "responses.conversation" "responses.previous_response_id" ]);
            default = [ ];
            description = "Responses protocol identifiers used to pin a stateful route.";
          };
          ttl = mkOption {
            type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
            default = null;
            description = "Affinity mapping TTL.";
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
          # race / retry
          count = mkOption {
            type = types.int;
            default = 0;
            description = ''
              Race batch size (0 = the whole pool) or retry scope="next" batch size.
            '';
          };
          scope = mkOption {
            type = types.enum [ "same" "next" ];
            default = "same";
            description = ''
              Retry scope: "same" repeats the original selection, "next" uses
              the next unused ranked targets and never repeats a used provider.
            '';
          };
          attempts = mkOption { type = types.ints.unsigned; default = 0; };
          on = mkOption {
            type = types.listOf (types.enum [ "timeout" "connection_error" "429" "5xx" "404" "invalid_response" "model_not_found" ]);
            default = [ ];
            description = "Error classes that trigger retry or fallback.";
          };
          backoffType = mkOption { type = types.enum [ "constant" "exponential" ]; default = "exponential"; };
          backoffInitial = mkOption { type = types.strMatching "[0-9]+(ms|s|m|h)"; default = "100ms"; };
          backoffMax = mkOption { type = types.strMatching "[0-9]+(ms|s|m|h)"; default = "1s"; };
          # hedge
          after = mkOption {
            type = types.nullOr (types.strMatching "[0-9]+(ms|s|m|h)");
            default = null;
            description = "Delay after which the next retry batch may start before the current branches complete.";
          };
          # semaphore
          maxCalls = mkOption {
            type = types.int;
            default = 0;
            description = "Total upstream calls allowed per client request.";
          };
          maxInFlight = mkOption {
            type = types.int;
            default = 0;
            description = "Simultaneously executing upstream calls per client request.";
          };
          maxCallsPerProvider = mkOption {
            type = types.int;
            default = 0;
            description = "Upstream calls to one provider inside a client request.";
          };
          fallbackStrategy = mkOption { type = types.enum [ "serial" "race" "hedge" ]; default = "serial"; };
        };
      });
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
