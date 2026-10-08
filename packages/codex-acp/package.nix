{ lib, buildPnpmCli, writeShellApplication, coreutils }:

let
  adapter = buildPnpmCli {
    pname = "codex-acp";
    version = "2.1.1";
    package = "@agentclientprotocol/codex-acp";
    pnpmLock = ./pnpm-lock.yaml;
    pnpmWorkspace = ./pnpm-workspace.yaml;
    pnpmDepsHash = "sha256-MfkVQnoVv1Hf2FyklQ+X0noDa9W1SqOgc1jty6gDY/w=";
    executables.codex-acp = "dist/index.js";
    description = "ACP adapter for Codex App Server";
    homepage = "https://github.com/agentclientprotocol/codex-acp";
    license = lib.licenses.asl20;
    mainProgram = "codex-acp";
    versionCheckOutput = "@agentclientprotocol/codex-acp 2.1.1";
  };
  sessionConfig = {
    cli_auth_credentials_store = "file";
    web_search = "disabled";
    features = {
      shell_tool = false;
      multi_agent = false;
      image_generation = false;
      code_mode = false;
      code_mode_only = false;
      tool_search = false;
      tool_suggest = false;
      goals = false;
      apps = false;
    };
  };
in
writeShellApplication {
  name = "codex-acp-fsless";
  runtimeInputs = [ coreutils ];
  text = ''
    : "''${CODEX_HOME:?a dedicated CODEX_HOME is required}"
    mkdir -p "$CODEX_HOME"
    chmod 0700 "$CODEX_HOME"
    ln -sfn ${./environments.toml} "$CODEX_HOME/environments.toml"
    # The file is authoritative; no legacy environment selector or alternate binary.
    unset CODEX_EXEC_SERVER_URL CODEX_PATH
    export CODEX_CONFIG=${lib.escapeShellArg (builtins.toJSON sessionConfig)}
    exec ${lib.getExe adapter} "$@"
  '';
  passthru = { inherit adapter; };
}
