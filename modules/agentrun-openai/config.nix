{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.agentrun-openai;
  stateDir = "/var/lib/${cfg.stateDirectory}";
  runtimeDir = "/run/${cfg.runtimeDirectory}";
  # Session store defaults under the StateDirectory so native sessions outlive
  # gateway restarts through systemd's StateDirectory (and, on the node,
  # impermanence persistence).
  sessionStore = if cfg.sessionStoreFile == null
    then "${stateDir}/sessions.json"
    else cfg.sessionStoreFile;

  baseArgs = [
    "--host" cfg.host
    "--port" (toString cfg.port)
    "--turn-timeout" cfg.turnTimeout
    "--session-ttl" cfg.sessionTtl
    "--stream-heartbeat" cfg.streamHeartbeat
    "--claude-thinking-budget" (toString cfg.claudeThinkingBudget)
    "--claude-binary" cfg.claudeBinary
    "--codex-acp-binary" cfg.codexAcpBinary
    "--agy-binary" cfg.agyBinary
    "--session-store" sessionStore
  ] ++ lib.optionals (cfg.defaultCwd != null) [
    "--default-cwd" (toString cfg.defaultCwd)
  ] ++ lib.optionals (cfg.allowedRoots != []) (
    lib.flatten (map (root: [ "--allowed-root" (toString root) ]) cfg.allowedRoots)
  );

  # CLI flags cannot reference env vars (systemd ExecStart would pass them
  # literally), so when an api key is set we wrap ExecStart in a real shell
  # script that materializes the key from the LoadCredential directory into
  # argv. Without a key the binary runs directly. The script also builds the
  # session PATH: declared `path` packages first, then the NixOS system
  # profile (so `nix` and system tools stay available inside sessions),
  # keeping the pi-acp-daemon PATH convention without fighting systemd's
  # default unit PATH.
  sessionPath = lib.makeBinPath cfg.path
    + ":/run/current-system/sw/bin:/run/current-system/sw/sbin";

  gatewayWrapper = pkgs.writeShellScript "agentrun-openai-wrapper" (
    ''
      export PATH=${lib.escapeShellArg sessionPath}:"$PATH"
    ''
    + (if cfg.apiKeyFile == null then ''
      exec ${lib.getExe cfg.package} ${lib.escapeShellArgs baseArgs}
    '' else ''
      set -eu
      test -r "$CREDENTIALS_DIRECTORY/api-key" || { echo "agentrun-openai: api key credential missing" >&2; exit 1; }
      api_key=$(tr -d '\r\n' < "$CREDENTIALS_DIRECTORY/api-key")
      exec ${lib.getExe cfg.package} ${lib.escapeShellArgs baseArgs} --api-key "$api_key"
    '')
  );
in
{
  config = lib.mkIf cfg.enable {
    # Прозрачная граница: реальные CLI argv доступны опцией commandLineArgs
    # (аналог llm-gateway.publicConfigFile), чтобы тесты могли проверить
    # сгенерированную команду без разбора ExecStart-обёртки.
    lattice.agentrun-openai.commandLineArgs = baseArgs;
    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
      description = "agentrun-openai OpenAI gateway user";
    };

    environment.systemPackages = [ cfg.package ];

    systemd.services.agentrun-openai = {
      description = "OpenAI-compatible HTTP gateway over agentrun";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      # extraEnv reaches the gateway (and thus every spawned agent CLI); the
      # session PATH is built inside the wrapper script above.
      environment = cfg.extraEnv;

      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = cfg.stateDirectory;
        StateDirectoryMode = "0700";
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        RuntimeDirectoryPreserve = "restart";
        WorkingDirectory = stateDir;
        UMask = "0077";
        ExecStart = gatewayWrapper;
        Restart = "on-failure";
        RestartSec = 5;

        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
        LockPersonality = true;
        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProtectSystem = "full";
        ProtectHome = true;
        ReadWritePaths = stateDir;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
      } // lib.optionalAttrs (cfg.apiKeyFile != null) {
        LoadCredential = [ "api-key:${toString cfg.apiKeyFile}" ];
      };
    };
  };
}
