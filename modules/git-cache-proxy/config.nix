{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.git-cache-proxy;

  # The proxy reads every flag from an env var (GITCACHEPROXY_*). We exec it
  # through a tiny wrapper so optional credentials are injected as environment
  # variables from systemd credential files — never via argv, and only when a
  # credential is actually mounted. `$CREDENTIALS_DIRECTORY` is populated by
  # systemd from LoadCredential.
  execScript = pkgs.writeShellScript "git-cache-proxy-exec" ''
    set -eu

    bind="${cfg.host}:${toString cfg.port}"
    export GITCACHEPROXY_BIND="$bind"
    export GITCACHEPROXY_CACHE_ROOT="${cfg.cacheRoot}"
    export GITCACHEPROXY_UPSTREAM="${cfg.upstream}"
    export GITCACHEPROXY_FETCH_TTL_SECONDS="${toString cfg.fetchTtlSeconds}"
    export GITCACHEPROXY_CACHE_MAX_MB="${toString cfg.cacheMaxMb}"
    export GITCACHEPROXY_MAX_CONCURRENT_REQUESTS="${toString cfg.maxConcurrentRequests}"
    export GITCACHEPROXY_MAX_DECODED_BODY_MB="${toString cfg.maxDecodedBodyMb}"

    if [ -n "''${CREDENTIALS_DIRECTORY-}" ] && [ -f "$CREDENTIALS_DIRECTORY/upstream-auth-header" ]; then
      secret="$(cat "$CREDENTIALS_DIRECTORY/upstream-auth-header")"
      # strip a single trailing newline, reject embedded newlines
      secret="''${secret%$'\n'}"
      case "$secret" in
        *$'\n'*) echo "invalid upstream-auth-header credential" >&2; exit 1 ;;
      esac
      export GITCACHEPROXY_UPSTREAM_AUTH_HEADER="$secret"
    fi

    if [ -n "''${CREDENTIALS_DIRECTORY-}" ] && [ -f "$CREDENTIALS_DIRECTORY/serve-token" ]; then
      token="$(cat "$CREDENTIALS_DIRECTORY/serve-token")"
      token="''${token%$'\n'}"
      case "$token" in
        *$'\n'*) echo "invalid serve-token credential" >&2; exit 1 ;;
      esac
      export GITCACHEPROXY_SERVE_TOKEN="$token"
    fi

    exec ${lib.getExe cfg.package}
  '';
in
{
  config = lib.mkIf cfg.enable {
    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
    };

    systemd.tmpfiles.rules = [
      "d ${toString cfg.cacheRoot} 0700 ${cfg.user} ${cfg.group} - -"
    ];

    systemd.services.git-cache-proxy = {
      description = "Lattice read-only Git cache proxy";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        WorkingDirectory = toString cfg.cacheRoot;
        ExecStart = execScript;
        LoadCredential = lib.concatLists [
          (lib.optionals (cfg.upstreamAuthHeaderFile != null)
            [ "upstream-auth-header:${toString cfg.upstreamAuthHeaderFile}" ])
          (lib.optionals (cfg.serveTokenFile != null)
            [ "serve-token:${toString cfg.serveTokenFile}" ])
        ];
        Restart = "on-failure";
        RestartSec = 5;
        TimeoutStartSec = 30;
        UMask = "0077";

        # Hardening: the proxy is a shared credentialed reader, so the trust
        # boundary is reachability + the operator-controlled Caddy ingress.
        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHome = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProtectSystem = "strict";
        ReadWritePaths = [ (toString cfg.cacheRoot) ];
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
