{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.attic;

  inherit (pkgs) formats;
  toml = formats.toml { };

  # Canonical API / substituter endpoint for clients. `publicUrl` is the cache
  # base clients reach (e.g. `https://cache.lattice.local/`); it may or may not
  # carry a trailing slash. We strip a trailing slash before composing paths
  # so the API endpoint keeps exactly one and the substituter URL is
  # `<base>/<cacheName>` without a doubled slash.
  baseUrl =
    if cfg.publicUrl != null
    then lib.removeSuffix "/" cfg.publicUrl
    else "http://${cfg.host}:${toString cfg.port}";

  apiEndpoint = "${baseUrl}/";

  # Secret-free server config: typed options only. The JWT admin-token secret
  # (if any) is deliberately omitted here and injected at runtime from the
  # LoadCredential-mounted file via the wrapper's environment, so it never
  # lands in the Nix store. atticd falls back to the
  # ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64 / ATTIC_SERVER_TOKEN_RS256_SECRET_BASE64
  # environment variables when the TOML leaves [jwt.signing] unset.
  serverToml = toml.generate "attic-server.toml" {
    listen = "${cfg.host}:${toString cfg.port}";
    allowed-hosts = [ ];
    api-endpoint = apiEndpoint;
    substituter-endpoint = apiEndpoint;
    database = {
      url = "sqlite://${toString cfg.dataRoot}/server.db?mode=rwc";
    };
    storage = {
      type = "local";
      path = "${toString cfg.dataRoot}/storage";
    };
    chunking = {
      nar-size-threshold = 64 * 1024;
      min-size = 16 * 1024;
      avg-size = 64 * 1024;
      max-size = 256 * 1024;
    };
    compression = {
      type = "zstd";
    };
    garbage-collection = {
      # 12 hours default; ops can override per-cache via `attic cache configure`.
      interval = "12 hours";
    };
  };

  # Wrapper injects the JWT token secret from the systemd credential into the
  # environment (exactly the variable atticd reads when [jwt.signing] is unset),
  # then execs atticd. The secret is an EnvironmentFile fragment
  # (`ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64="<base64>"`), matching attic's
  # upstream NixOS module. The server signing keypair is NOT a file: attic
  # generates and stores it server-side in its database (see README).
  execScript = pkgs.writeShellScript "atticd-exec" ''
    set -eu

    if [ -n "''${CREDENTIALS_DIRECTORY-}" ] && [ -f "$CREDENTIALS_DIRECTORY/token-secret" ]; then
      # EnvironmentFile semantics: export every KEY="value" line.
      set -a
      . "$CREDENTIALS_DIRECTORY/token-secret"
      set +a
    fi

    exec ${lib.getExe cfg.package} -f ${serverToml} --mode monolithic
  '';

  # Standard nix substituter URL for clients.
  substituterUrl =
    if cfg.publicUrl != null
    then "${baseUrl}/${cfg.cacheName}"
    else "http://${cfg.host}:${toString cfg.port}/${cfg.cacheName}";
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      # The JWT admin-token secret must never live in the world-readable Nix
      # store; the runtime credential path must point outside it. Mirrors
      # attic's upstream NixOS module assertion.
      {
        assertion = cfg.tokenSecretFile == null || !lib.isStorePath (toString cfg.tokenSecretFile);
        message = ''
          lattice.attic.tokenSecretFile must not point into the Nix store
          (the store is world-readable). Use a quoted absolute path to an
          agenix-decrypted secret, e.g. config.age.secrets.<name>.path.
        '';
      }
      # A client trust key, when provided, must be in canonical Nix form
      # `<keyName>:<base64>` with no whitespace (cache.nixos.org style).
      {
        assertion =
          cfg.trustedPublicKey == null
          || (lib.hasInfix ":" cfg.trustedPublicKey
              && builtins.match ".*[ \t\n].*" cfg.trustedPublicKey == null);
        message = ''
          lattice.attic.trustedPublicKey must be the cache's public signing
          key in canonical Nix form `<keyName>:<base64>` (as printed by
          `attic cache info`). Got: ${toString cfg.trustedPublicKey}.
        '';
      }
      # Client wiring requires both a trust key and a canonical client URL
      # when substituter is on. A key without a URL is a silent no-op and a
      # URL without a key would let the node accept unsigned nars, so both
      # partial states are rejected here. The all-null placeholder (key and
      # URL unset, substituter on) is the documented pre-deploy no-op and must
      # evaluate cleanly.
      {
        assertion =
          !cfg.substituter
          || cfg.trustedPublicKey == null
          || cfg.publicUrl == null;
        message = ''
          lattice.attic: with substituter = true you must set BOTH
          trustedPublicKey and publicUrl, or keep both null (pre-deploy
          placeholder = no client wiring emitted). The node's own nix accepts
          ONLY nars signed by the cache key, so a key without a URL or a URL
          without a key is a configuration error.
        '';
      }
    ];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
    };

    systemd.tmpfiles.rules = [
      "d ${toString cfg.dataRoot} 0700 ${cfg.user} ${cfg.group} - -"
    ];

    systemd.services.attic = {
      description = "Lattice Attic Nix binary cache (atticd)";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        WorkingDirectory = toString cfg.dataRoot;
        ExecStart = execScript;
        LoadCredential = lib.optional (cfg.tokenSecretFile != null)
          "token-secret:${toString cfg.tokenSecretFile}";
        Restart = "on-failure";
        RestartSec = 10;
        TimeoutStartSec = 30;
        UMask = "0077";

        # Strict sandbox (mirrors git-cache-proxy / attic upstream): no
        # privileges, write access limited to the persistent dataRoot.
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
        ReadWritePaths = [ (toString cfg.dataRoot) ];
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

    # Client substituter wiring: the node's own nix fetches through the local
    # attic and accepts ONLY nars signed by the cache key. Wired only when a
    # trust key and a canonical URL are known (placeholder -> no-op otherwise).
    nix.settings = lib.mkIf (cfg.substituter && cfg.trustedPublicKey != null && cfg.publicUrl != null) {
      substituters = [ substituterUrl ];
      trusted-public-keys = [ cfg.trustedPublicKey ];
    };

    environment.systemPackages =
      lib.optional (cfg.clientPackage != null) cfg.clientPackage;

    # Read-only escape hatch for tests/ops to inspect the generated TOML.
    lattice.attic.configFile = serverToml;
  };
}
