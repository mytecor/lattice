{ lib, writeShellApplication, coreutils, nodejs, muse, muse-acp }:

let
  # Shared by the ACP process and the operator's login command. Never use the
  # gateway's HOME/XDG directories, which are shared with other agent CLIs.
  environment = ''
    : "''${MUSE_HOME:?a dedicated MUSE_HOME is required}"
    export HOME="$MUSE_HOME"
    export XDG_CONFIG_HOME="$HOME/config"
    export XDG_DATA_HOME="$HOME/data"
    export XDG_STATE_HOME="$HOME/state"
    export XDG_CACHE_HOME="$HOME/cache"
    mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME" "$XDG_CACHE_HOME"
    chmod 0700 "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME" "$XDG_CACHE_HOME"
  '';
  cli = writeShellApplication {
    name = "muse-agentrun";
    runtimeInputs = [ coreutils ];
    text = environment + ''
      exec ${lib.getExe muse} "$@"
    '';
  };
in
writeShellApplication {
  name = "muse-acp-fsless";
  runtimeInputs = [ coreutils ];
  text = environment + ''
    export MUSE_CLI=${lib.getExe muse}
    # The supervisor chooses an empty toolset or only the gateway MCP bridge,
    # and overwrites caller-supplied host flags before starting the adapter.
    exec ${lib.getExe nodejs} ${./fsless.mjs} ${lib.getExe muse-acp} "$@"
  '';
  passthru = { inherit cli; };
  meta = {
    description = "Muse ACP backend with built-in tools disabled and isolated state";
    mainProgram = "muse-acp-fsless";
    platforms = lib.platforms.linux;
  };
}
