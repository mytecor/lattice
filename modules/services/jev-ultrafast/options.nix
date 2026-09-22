{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  # F18: the Jev browser agent (upstream browser-use/jev-ultrafast). Connects
  # to the browser ONLY through BU_CDP_URL → the Foxbridge/Camoufox runtime.
  # The agent loop and policy are the original upstream code (key F18
  # requirement) — no local fork, no replaced snapshot.js.
  options.lattice.jev-ultrafast = {
    enable = mkEnableOption "the Jev ultrafast browser agent (F18)";

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.jev-ultrafast;
      defaultText = lib.literalExpression "pkgs.lattice.jev-ultrafast";
      description = "The Jev agent package to run (upstream jev-ultrafast + pinned deps).";
    };

    user = mkOption {
      type = types.str;
      default = "jev";
      description = "System user running the agent.";
    };

    group = mkOption {
      type = types.str;
      default = "jev";
      description = "System group of the agent user.";
    };

    # The one and only link to the browser: the Foxbridge CDP endpoint.
    cdpUrl = mkOption {
      type = types.str;
      default = "http://127.0.0.1:9222";
      description = ''
        Browser endpoint Jev's browser-harness connects to (BU_CDP_URL). Must
        point at the loopback Foxbridge CDP endpoint — never a published
        address.
      '';
    };

    # Loopback-only HTTP inspector (Jev upstream binds 127.0.0.1 itself).
    inspectorPort = mkOption {
      type = types.port;
      default = 8766;
      description = "Loopback TCP port of the Jev inspector HTTP server (TYPESAFE_DEMO_PORT).";
    };

    runtimeDirectory = mkOption {
      type = types.str;
      default = "jev-ultrafast";
      description = "systemd RuntimeDirectory name (private /run dir) for the disposable HOME/profile.";
    };

    textModel = mkOption {
      type = types.str;
      default = "deepseek-chat";
      description = "Text model id used by the Jev executor (TEXT_MODEL).";
    };

    textModelBaseUrl = mkOption {
      type = types.str;
      default = "https://api.deepseek.com/v1";
      description = "OpenAI-compatible base URL for the text model (TEXT_MODEL_BASE_URL).";
    };

    # Jev API keys — NEVER in the Nix store. Runtime paths (agenix secrets),
    # mounted via systemd LoadCredential and injected by the exec wrapper.
    typesafeApiKeyFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path to the TYPESAFE_API_KEY content (e.g. an agenix secret).
        Mounted via systemd LoadCredential, never in argv or the store. When
        null the agent still runs (inspector mode) but model tasks need the
        key.
      '';
    };

    textModelApiKeyFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path to the TEXT_MODEL_API_KEY content (e.g. an agenix secret).
        Mounted via systemd LoadCredential, never in argv or the store. When
        null, TEXT tasks need the key.
      '';
    };
  };
}
