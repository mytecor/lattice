{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.authentik;
  ak = "${cfg.package}/bin/ak";

  # Runtime-writable home for the authentik service user (media/storage).
  dataDir = toString cfg.dataDir;

  # Non-secret Authentik configuration. Secrets (SECRET_KEY, bootstrap
  # password/token) are NOT here: each is an agenix file that systemd loads as
  # an extra EnvironmentFile entry (one `AUTHENTIK_*=...` line per file), so no
  # value ever lands in the Nix store or in this module's generated text.
  #
  # Listeners: Authentik defaults to binding HTTP/HTTPS/LDAP/RADIUS/metrics on
  # `[::]` (f14 hard-constraint: non-public). We pin only the HTTP listener to
  # the operator-chosen loopback address:port and set every other listener to
  # an empty list so nothing opens on the host.
  listenEnv = {
    AUTHENTIK_LISTEN__HTTP = "${cfg.listenAddress}:${toString cfg.port}";
    AUTHENTIK_LISTEN__HTTPS = "";
    AUTHENTIK_LISTEN__LDAP = "";
    AUTHENTIK_LISTEN__LDAPS = "";
    AUTHENTIK_LISTEN__RADIUS = "";
    AUTHENTIK_LISTEN__METRICS = "";
    AUTHENTIK_LISTEN__DEBUG = "";
    AUTHENTIK_LISTEN__DEBUG_PY = "";
  };

  # Loopback PostgreSQL over the unix socket. nixpkgs postgresql's default
  # pg_hba authenticates `local all all peer`, i.e. the OS user name must equal
  # the DB role name. Our service runs as the `authentik` system user and the
  # module creates the matching `authentik` role — so no password is needed and
  # no DB credential ever enters the Nix store. (2026.5.6 uses
  # django_postgres_cache, so there is no Redis dependency either.)
  dbEnv = {
    AUTHENTIK_POSTGRESQL__HOST = "/run/postgresql";
    AUTHENTIK_POSTGRESQL__NAME = cfg.dbUser;
    AUTHENTIK_POSTGRESQL__USER = cfg.dbUser;
    AUTHENTIK_POSTGRESQL__SSLMODE = "disable";
  };

  # The `ak` wrapper prepends the bundled python environment to PATH so
  # `ak manage ...` / `ak server` / `ak worker` resolve their tools. We run the
  # units as the unprivileged `authentik` user: the wrapper then skips its
  # container-root branch (docker socket, chown /data /certs, setpriv) and execs
  # the binary directly as that user.
  commonEnv = listenEnv // dbEnv // {
    AUTHENTIK_DEBUG = "false";
    AUTHENTIK_LOG_LEVEL = cfg.logLevel;
    AUTHENTIK_DISABLE_UPDATE_CHECK = "true";
    AUTHENTIK_DISABLE_STARTUP_ANALYTICS = "true";
    AUTHENTIK_ERROR_REPORTING__ENABLED = "false";
    AUTHENTIK_STORAGE__FILE__PATH = dataDir;
  };

  # agenix secret files, each a single `AUTHENTIK_*=...` line, loaded as extra
  # EnvironmentFile entries. secretKeyFile is mandatory (module asserts);
  # bootstrap files are required for the operator account. systemd merges
  # multiple EnvironmentFile entries in order, later files override earlier.
  envFiles = [ cfg.secretKeyFile ] ++ lib.optionals (cfg.bootstrapTokenFile != null) [
    cfg.bootstrapTokenFile
  ] ++ lib.optionals (cfg.bootstrapUserFile != null) [
    cfg.bootstrapUserFile
  ] ++ lib.optionals (cfg.bootstrapEmailFile != null) [
    cfg.bootstrapEmailFile
  ] ++ lib.optionals (cfg.bootstrapPasswordFile != null) [
    cfg.bootstrapPasswordFile
  ];
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.secretKeyFile != null;
        message = ''
          lattice.authentik: secretKeyFile must be set (an agenix secret holding
          `AUTHENTIK_SECRET_KEY=...`). Authentik's Django SECRET_KEY has no
          default; refusing to run with a plaintext store value.
        '';
      }
      {
        assertion = cfg.bootstrapTokenFile != null;
        message = ''
          lattice.authentik: bootstrapTokenFile must be set (an agenix secret
          holding `AUTHENTIK_BOOTSTRAP_TOKEN=...`). This creates the operator
          API token (intent=api, expiring=false) used for declarative
          provisioning. Provide lattice.authentik.bootstrapTokenFile.
        '';
      }
      {
        assertion = lib.all (f: f != null) [ cfg.bootstrapUserFile cfg.bootstrapEmailFile cfg.bootstrapPasswordFile ];
        message = ''
          lattice.authentik: bootstrapUserFile, bootstrapEmailFile and
          bootstrapPasswordFile must all be set to create the initial operator
          account (AUTHENTIK_BOOTSTRAP_USERNAME/_EMAIL/_PASSWORD).
        '';
      }
    ];

    # The OS-level service user doubles as the PostgreSQL role name so default
    # peer auth over the unix socket needs no password.
    users.users.${cfg.dbUser} = {
      isSystemUser = true;
      group = cfg.dbUser;
      home = dataDir;
      createHome = true;
    };
    users.groups.${cfg.dbUser} = { };

    services.postgresql = {
      enable = true;
      # ensureUsers creates the `authentik` role; peer auth (OS user == DB user,
      # unix socket) needs no password. ensureDBOwnership grants the database
      # `authentik` to the `authentik` role.
      ensureDatabases = [ cfg.dbUser ];
      ensureUsers = [
        {
          name = cfg.dbUser;
          ensureDBOwnership = true;
        }
      ];
      # Default nixpkgs pg_hba already has `local all all peer`; the host rows
      # below keep TCP loopback available for local tooling but our service uses
      # the unix socket (peer) exclusively. No credential is invented here.
      authentication = lib.mkAfter ''
        host  all all 127.0.0.1/32 scram-sha-256
        host  all all ::1/128      scram-sha-256
      '';
    };

    # ---- migrations (one-shot, before server/worker) ----
    systemd.services.authentik-migrate = {
      description = "Authentik database migrations";
      wantedBy = [ "multi-user.target" ];
      before = [ "authentik-server.service" "authentik-worker.service" ];
      requires = [ "postgresql.service" ];
      after = [ "postgresql.service" ];
      serviceConfig = {
        Type = "oneshot";
        User = cfg.dbUser;
        Group = cfg.dbUser;
        WorkingDirectory = dataDir;
        EnvironmentFile = envFiles;
        ExecStart = "${ak} manage migrate";
      };
      environment = commonEnv;
    };

    systemd.services.authentik-server = {
      description = "Authentik SSO server (loopback)";
      wantedBy = [ "multi-user.target" ];
      requires = [ "authentik-migrate.service" ];
      after = [ "authentik-migrate.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = commonEnv;
      serviceConfig = {
        User = cfg.dbUser;
        Group = cfg.dbUser;
        WorkingDirectory = dataDir;
        EnvironmentFile = envFiles;
        ExecStart = "${ak} server";
        Restart = "on-failure";
        RestartSec = 5;
        # Harden: loopback HTTP only; no ambient caps/root.
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = false;
        ReadWritePaths = [ dataDir ];
      };
    };

    systemd.services.authentik-worker = {
      description = "Authentik worker (background tasks)";
      wantedBy = [ "multi-user.target" ];
      requires = [ "authentik-migrate.service" ];
      after = [ "authentik-migrate.service" "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = commonEnv;
      serviceConfig = {
        User = cfg.dbUser;
        Group = cfg.dbUser;
        WorkingDirectory = dataDir;
        EnvironmentFile = envFiles;
        ExecStart = "${ak} worker";
        Restart = "on-failure";
        RestartSec = 5;
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = false;
        ReadWritePaths = [ dataDir ];
      };
    };

    # Authentik's loopback port is intentionally NOT added to the firewall: the
    # single external ingress is Caddy (profiles/tcp-gateway), which proxies to
    # Authentik over loopback without a rule (same contract as Grafana, F12).
  };
}
