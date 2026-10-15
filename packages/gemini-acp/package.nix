{ lib
, gemini-cli
, nodejs
, coreutils
, writeShellApplication
}:

let
  adapter = writeShellApplication {
    name = "gemini-acp";
    runtimeInputs = [ gemini-cli ];
    text = ''
      exec ${lib.getExe gemini-cli} --acp "$@"
    '';
    meta = {
      description = "Google Gemini CLI ACP server";
      homepage = "https://github.com/google-gemini/gemini-cli";
      license = lib.licenses.asl20;
      mainProgram = "gemini-acp";
    };
  };
in
writeShellApplication {
  name = "gemini-acp-fsless";
  runtimeInputs = [ coreutils nodejs gemini-cli ];
  text = ''
    : "''${GEMINI_HOME:?a dedicated GEMINI_HOME is required}"
    export GEMINI_CLI_HOME="$GEMINI_HOME"
    mkdir -p "$GEMINI_CLI_HOME/.gemini/policies"
    chmod 0700 "$GEMINI_CLI_HOME" "$GEMINI_CLI_HOME/.gemini"

    ln -sfn ${./fsless.toml} "$GEMINI_CLI_HOME/.gemini/policies/fsless.toml"

    if [ ! -f "$GEMINI_CLI_HOME/.gemini/settings.json" ]; then
      if [ -n "''${GOOGLE_GEMINI_BASE_URL:-}" ]; then
        auth_type="gateway"
      elif [ -n "''${GOOGLE_API_KEY:-}" ] || [ -n "''${GOOGLE_CLOUD_PROJECT:-}" ]; then
        auth_type="vertex-ai"
      else
        auth_type="gemini-api-key"
      fi
      cat > "$GEMINI_CLI_HOME/.gemini/settings.json" <<EOF
{
  "tools": {
    "exclude": [
      "run_shell_command",
      "list_background_processes",
      "read_background_output",
      "read_file",
      "read_many_files",
      "write_file",
      "replace",
      "list_directory",
      "glob",
      "grep_search",
      "search_file_content",
      "google_web_search",
      "web_fetch",
      "activate_skill",
      "get_internal_docs",
      "enter_plan_mode",
      "exit_plan_mode",
      "tracker_create_task",
      "tracker_update_task",
      "tracker_get_task",
      "tracker_list_tasks",
      "tracker_add_dependency",
      "tracker_visualize",
      "update_topic",
      "write_todos",
      "ask_user",
      "invoke_agent"
    ]
  },
  "security": {
    "auth": {
      "selectedType": "$auth_type"
    }
  }
}
EOF
      chmod 0600 "$GEMINI_CLI_HOME/.gemini/settings.json"
    fi

    export GEMINI_CLI_TRUST_WORKSPACE="true"
    exec ${lib.getExe nodejs} ${./filter.mjs} ${lib.getExe gemini-cli} --acp --admin-policy "${./fsless.toml}" "$@"
  '';
  passthru = { inherit adapter; };
}
