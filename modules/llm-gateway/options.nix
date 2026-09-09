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
          accessGroup = mkOption {
            type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
            default = name;
            description = "Access group used for logical model mappings and same-group fallback.";
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

    models = mkOption {
      default = [ ];
      description = "Logical model primaries, one native model per logical ID and access group.";
      type = types.listOf (types.submodule {
        options = {
          logical = mkOption { type = types.str; };
          accessGroup = mkOption { type = types.str; };
          native = mkOption { type = types.str; };
        };
      });
    };

    routingRules = mkOption {
      default = [ ];
      description = "Flat ordered routing pipeline for logical models.";
      type = types.listOf (types.submodule {
        options = {
          model = mkOption { type = types.str; };
          action = mkOption { type = types.enum [ "race" "retry" "fallback" "timeout" "hedge" ]; };
          accessGroups = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Virtual provider access groups selected by route-creating actions such as race and fallback.";
          };
          attempts = mkOption { type = types.ints.unsigned; default = 0; };
          on = mkOption {
            type = types.listOf (types.enum [ "timeout" "connection_error" "429" "5xx" "invalid_response" ]);
            default = [ ];
          };
          backoffType = mkOption { type = types.enum [ "constant" "exponential" ]; default = "exponential"; };
          backoffInitial = mkOption { type = types.strMatching "[0-9]+(ms|s|m|h)"; default = "100ms"; };
          backoffMax = mkOption { type = types.strMatching "[0-9]+(ms|s|m|h)"; default = "1s"; };
          duration = mkOption { type = types.nullOr types.str; default = null; };
          after = mkOption { type = types.nullOr types.str; default = null; };
          fallbackStrategy = mkOption { type = types.enum [ "serial" "race" "hedge" ]; default = "serial"; };
        };
      });
    };

    publicConfigFile = mkOption {
      type = types.path;
      readOnly = true;
      description = "Generated non-secret gateway configuration template in the Nix store.";
    };
  };
}
