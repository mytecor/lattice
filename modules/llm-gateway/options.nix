{ lib, pkgs, ... }:

let
  inherit (lib) mkOption types;
  inboundFormat = types.enum [
    "openai_chat"
    "openai_responses"
    "anthropic_messages"
    "gemini"
  ];
in
{
  options.lattice.llm-gateway = {
    enable = mkOption {
      type = types.bool;
      default = false;
      description = "Enable the headless token_proxy LLM gateway.";
    };

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.token-proxy;
      defaultText = lib.literalExpression "pkgs.lattice.token-proxy";
      description = "Package providing the token-proxy binary.";
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
      description = "token_proxy log level. Silent is the secure production default.";
    };

    modelListPrefix = mkOption {
      type = types.bool;
      default = false;
      description = "Expose upstream-prefixed model IDs from /v1/models.";
    };

    logicalModels = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = ''
        Public model IDs allowed at the client boundary. When non-empty, every advertised model
        must belong to this set and every logical model must have an upstream mapping.
      '';
    };

    retryableFailureCooldownSeconds = mkOption {
      type = types.ints.unsigned;
      default = 15;
      description = "Cooldown applied to an upstream after retryable failures.";
    };

    sameUpstreamRetryCount = mkOption {
      type = types.ints.between 0 5;
      default = 1;
      description = "Extra attempts on the same upstream before failover.";
    };

    routing = {
      order = mkOption {
        type = types.enum [ "fill_first" "round_robin" ];
        default = "fill_first";
        description = "Candidate order within a priority group.";
      };

      dispatch = mkOption {
        type = types.enum [ "serial" "hedged" "race" ];
        default = "serial";
        description = "Dispatch strategy within a priority group.";
      };

      hedgeDelayMs = mkOption {
        type = types.ints.positive;
        default = 2000;
        description = "Delay before an additional hedged attempt starts.";
      };

      maxParallel = mkOption {
        type = types.ints.positive;
        default = 2;
        description = "Maximum parallel attempts for race or hedged dispatch.";
      };
    };

    upstreams = mkOption {
      default = { };
      description = "Declarative non-secret upstream routing configuration.";
      type = types.attrsOf (types.submodule ({ name, ... }: {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = "Include this upstream in runtime configuration.";
          };

          id = mkOption {
            type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
            default = name;
            description = "Stable non-secret upstream identifier.";
          };

          providers = mkOption {
            type = types.listOf (types.enum [
              "openai"
              "openai-response"
              "anthropic"
              "gemini"
            ]);
            default = [ ];
            description = "token_proxy provider protocols supported by this upstream.";
          };

          baseUrl = mkOption {
            type = types.str;
            description = "OpenAI-compatible upstream base URL.";
          };

          apiKeyFiles = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = ''
              Runtime paths to individual API key files. Use strings, not Nix paths, so secret
              contents cannot be copied into the Nix store.
            '';
          };

          priority = mkOption {
            type = types.int;
            default = 0;
            description = "Higher priority groups are attempted first.";
          };

          availableModels = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Inbound model allowlist advertised by this upstream.";
          };

          modelMappings = mkOption {
            type = types.attrsOf types.str;
            default = { };
            description = "Inbound logical model patterns mapped to provider-specific model IDs.";
          };

          convertFrom = mkOption {
            type = types.attrsOf (types.listOf inboundFormat);
            default = { };
            description = "Explicit inbound formats that each provider may convert from.";
          };

          proxyUrl = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "Optional outbound proxy URL.";
          };

          headerOverrides = mkOption {
            type = types.attrsOf (types.nullOr types.str);
            default = { };
            description = "Non-secret upstream header overrides; null removes a header.";
          };
        };
      }));
    };

    publicConfigFile = mkOption {
      type = types.path;
      readOnly = true;
      description = "Generated non-secret token_proxy configuration template in the Nix store.";
    };
  };
}
