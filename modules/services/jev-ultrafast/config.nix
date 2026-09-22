{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.jev-ultrafast;
  browserCfg = config.lattice.foxbridge-camoufox;

  # Jev reads plain env vars (os.environ.get), not *_FILE forms, so the secret
  # values are injected from systemd credential files by a tiny wrapper — never
  # via argv, only when a credential is actually mounted. `$CREDENTIALS_DIRECTORY`
  # is populated by systemd from LoadCredential. This mirrors the git-cache-proxy
  # credential injection pattern.
  execScript = pkgs.writeShellScript "jev-ultrafast-exec" ''
    set -eu

    export BU_CDP_URL="${cfg.cdpUrl}"
    export TYPESAFE_DEMO_PORT="${toString cfg.inspectorPort}"
    export TEXT_MODEL="${cfg.textModel}"
    export TEXT_MODEL_BASE_URL="${cfg.textModelBaseUrl}"
    # Isolate this service's browser-harness daemon from any manual daemon
    # (f18-04): BH_RUNTIME_DIR pins the IPC socket/port/pid stubs.
    export BH_RUNTIME_DIR="/run/${cfg.runtimeDirectory}/browser-harness"

    if [ -n "''${CREDENTIALS_DIRECTORY-}" ] && [ -f "$CREDENTIALS_DIRECTORY/typesafe-api-key" ]; then
      key="$(cat "$CREDENTIALS_DIRECTORY/typesafe-api-key")"
      key="''${key%$'\n'}"
      case "$key" in
        *$'\n'*) echo "invalid typesafe-api-key credential" >&2; exit 1 ;;
      esac
      export TYPESAFE_API_KEY="$key"
    fi

    if [ -n "''${CREDENTIALS_DIRECTORY-}" ] && [ -f "$CREDENTIALS_DIRECTORY/text-model-api-key" ]; then
      key="$(cat "$CREDENTIALS_DIRECTORY/text-model-api-key")"
      key="''${key%$'\n'}"
      case "$key" in
        *$'\n'*) echo "invalid text-model-api-key credential" >&2; exit 1 ;;
      esac
      export TEXT_MODEL_API_KEY="$key"
    fi

    # Disposable HOME (tmpfs): browser-harness writes config to
    # $HOME/.config/browser-harness and Jev caches .env etc. Kept out of
    # ProtectHome so the agent can write to its own private /run only.
    export HOME="/run/${cfg.runtimeDirectory}"

    exec ${lib.getExe cfg.package}
  '';

  # LoadCredential entries for the optional secrets, wired conditionally per
  # node convention (builtins.pathExists + lib.optionalAttrs to skip missing
  # .age files).
  credentials = lib.concatLists [
    (lib.optionals (cfg.typesafeApiKeyFile != null)
      [ "typesafe-api-key:${toString cfg.typesafeApiKeyFile}" ])
    (lib.optionals (cfg.textModelApiKeyFile != null)
      [ "text-model-api-key:${toString cfg.textModelApiKeyFile}" ])
  ];

  # System hard dependencies on the browser runtime. Requires+After keeps Jev
  # from ever starting before Foxbridge; ExecStartPre additionally probes the
  # live CDP /json/version so a slow Camoufox boot doesn't race the agent.
  requiresBrowser = browserCfg.enable;

in
{
  config = lib.mkIf cfg.enable {
    # The agent is meaningless without a live browser runtime. Refusing a
    # config where the browser module is disabled catches mis-wiring at eval
    # time, not after a quiet no-op start.
    assertions = [{
      assertion = requiresBrowser;
      message = ''
        lattice.jev-ultrafast: enable requires lattice.foxbridge-camoufox.enable
        — the agent connects to the browser only through its CDP endpoint.
      '';
    }];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
    };

    systemd.services.jev-ultrafast = {
      description = "Jev ultrafast browser agent (upstream jev-ultrafast, F18)";
      documentation = [ "https://github.com/browser-use/jev-ultrafast" ];
      # Jev is alive only while the browser runtime is alive.
      requires = lib.mkIf requiresBrowser [ "foxbridge-camoufox.service" ];
      after = lib.mkIf requiresBrowser [ "foxbridge-camoufox.service" ];
      wantedBy = [ "multi-user.target" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        # Disposable HOME/profile on tmpfs; recreated on boot. systemd creates
        # /run/jev-ultrafast before ExecStart, so browser-harness can mkdir.
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        WorkingDirectory = "/run/${cfg.runtimeDirectory}";

        ExecStart = execScript;
        ExecStartPre = lib.concatStringsSep "\n" [
          (pkgs.writeShellScript "jev-wait-cdp" ''
            i=0
            until ${pkgs.curl}/bin/curl -sf --max-time 2 http://127.0.0.1:${toString browserCfg.port}/json/version >/dev/null 2>&1; do
              i=$((i+1))
              [ $i -ge 30 ] && { echo "foxbridge not ready after 30s" >&2; exit 1; }
              sleep 1
            done
          '')
        ];

        LoadCredential = credentials;

        Restart = "on-failure";
        RestartSec = 3;
        TimeoutStartSec = 60;
        KillMode = "mixed";
        GuessMainPID = true;

        # Hardening: the agent is loopback HTTP + outbound HTTPS to model APIs.
        # No elevation; HOME is the private tmpfs runtime dir.
        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
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
        ProtectSystem = "strict";
        # HOME lives in the runtime dir; keep the rest of / home off-limits.
        ProtectHome = true;
        ReadWritePaths = [ "/run/${cfg.runtimeDirectory}" ];
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
          "~@resources"
        ];
      };
    };
  };
}
