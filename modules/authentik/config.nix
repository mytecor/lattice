{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.authentik;
  ak = "${cfg.package}/bin/ak";

  # One-shot migration entrypoint. The `ak` wrapper (run unprivileged as the
  # `authentik` system user) maps `ak manage migrate` to `python -m manage
  # manage migrate` — a leading `manage` is already injected by the wrapper's
  # non-root branch, so Django fails with `Unknown command: 'manage'` and the
  # schema is never created (server/worker then can't start → Caddy 502).
  # The correct full-migration entrypoint for this packaging is
  # `python -m lifecycle.migrate` (system migrations + Django migrate + check).
  # The python environment carrying `authentik-django` is built privately inside
  # nixpkgs's authentik derivation and not exposed as a public attribute, so we
  # read its store path out of the `ak` wrapper's baked PATH rather than
  # rebuilding it here (robust across nixpkgs rebuilds).
  migrate = pkgs.writeShellScript "authentik-migrate" ''
    set -euo pipefail
    ak="${ak}"
    python="$(sed -n "s|^PATH='\(/nix/store/[^']*\)/bin'\$PATH.*|\1|p" "$ak" |
      sed -n '1p')/bin/python"
    if [[ ! -x "$python" ]]; then
      echo "authentik-migrate: could not resolve python env from $ak" >&2
      exit 1
    fi
    exec "$python" -m lifecycle.migrate "$@"
  '';

  # Runtime-writable home for the authentik service user (media/storage).
  dataDir = toString cfg.dataDir;

  # Non-secret Authentik configuration. Secrets (SECRET_KEY, bootstrap
  # password/token) are NOT here: each is an agenix file that systemd loads as
  # an extra EnvironmentFile entry (one `AUTHENTIK_*=...` line per file), so no
  # value ever lands in the Nix store or in this module's generated text.
  #
  # Listeners: Authentik defaults to binding HTTP/HTTPS/LDAP/RADIUS/metrics on
  # `[::]` (f14 hard-constraint: non-public). The Python/Django server tolerates
  # an empty listener value as "don't bind", so we pin only the HTTP listener to
  # the operator-chosen loopback address:port and set every other listener to an
  # empty list so the server opens nothing on the host.
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
  } // lib.optionalAttrs blueprintEnabled {
    # nixpkgs substitutes Authentik's default /blueprints with the private
    # authentik-django store path.  This combined directory keeps all packaged
    # blueprints and adds the Nix-generated Lattice blueprint.
    AUTHENTIK_BLUEPRINTS_DIR = toString blueprintsDir;
  };

  # The Rust worker (`ak worker`, the 2026.5.x Dramatiq/healthcheck worker)
  # reads the SAME AUTHENTIK_LISTEN__* namespace as Django, but through
  # config_rs/serde instead of Python. Two differences both break a shared
  # listenEnv and are the reason the worker is split out here:
  #
  #  * config_rs parses listen.http and listen.metrics as comma-separated lists
  #    of SocketAddr. An empty string becomes [""] and fails to parse with
  #    "invalid socket address syntax" — the crash-loop this fix removes.
  #    (HTTPS/LDAP/LDAPS/RADIUS/DEBUG/DEBUG_PY are not fields in the Rust
  #    ListenConfig, so their empty values are harmless there.)
  #  * the worker actually binds listen.http and listen.metrics itself (its
  #    per-process healthcheck and metrics routers on TCP), so it must not share
  #    the server's ${cfg.port} on the same loopback address.
  #
  # Fix: give the worker valid, distinct loopback addresses. 127.0.0.1:0 is an
  # ephemeral port the kernel assigns at bind time — valid for the config parser
  # and guaranteed not to collide with the server's port or with each other.
  # Nothing external needs those TCP endpoints: the worker's health-live/ready
  # and metrics probes run over a unix socket, not TCP.
  workerEnv = commonEnv // {
    AUTHENTIK_LISTEN__HTTP = "${cfg.listenAddress}:0";
    AUTHENTIK_LISTEN__METRICS = "${cfg.listenAddress}:0";
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

  hostName = config.networking.hostName;
  meshDomain = config.lattice.tcp-gateway.meshDomain;
  blueprintEnabled = cfg.forwardAuth != [ ] || cfg.oidcApplications != [ ];
  lanOrigin = service: "http://${service}.${hostName}.local";
  meshScheme = if config.lattice.tcp-gateway.cloudflareToken != null then "https" else "http";
  meshOrigin = service:
    if meshDomain == null || lib.elem service config.lattice.tcp-gateway.meshExclude
    then null
    else "${meshScheme}://${service}.${meshDomain}";
  oidcApplications = map (app:
    let
      lan = lanOrigin app.service;
      mesh = meshOrigin app.service;
      origins = [ lan ] ++ lib.optional (mesh != null) mesh;
    in
    app // {
      launchUrl = "${if mesh != null then mesh else lan}/";
      redirectUris = map (origin: "${origin}${app.callbackPath}") origins;
    }
  ) cfg.oidcApplications;
  forwardAuthHosts = lib.concatMap (entry:
    let
      service = entry.service;
      lanDomain = "${hostName}.local";
      mkHost = suffix: host: cookieDomain: {
        slug = "${service}-fa${suffix}";
        inherit host cookieDomain;
      };
    in
    [ (mkHost "" "http://${service}.${lanDomain}" lanDomain) ]
    ++ lib.optional (meshDomain != null && !(lib.elem service config.lattice.tcp-gateway.meshExclude))
      (mkHost "-mesh" "${meshScheme}://${service}.${meshDomain}" meshDomain)
  ) cfg.forwardAuth;

  yamlString = builtins.toJSON;
  indent = prefix: value:
    lib.concatMapStringsSep "\n" (line: prefix + line) (lib.splitString "\n" value);
  providerEntry = host: ''
    - model: authentik_providers_proxy.proxyprovider
      id: ${yamlString "${host.slug}-provider"}
      state: present
      identifiers:
        name: ${yamlString host.slug}
      attrs:
        mode: forward_single
        external_host: ${yamlString host.host}
        cookie_domain: ${yamlString host.cookieDomain}
        authorization_flow: !Find [authentik_flows.flow, [slug, default-provider-authorization-explicit-consent]]
        invalidation_flow: !Find [authentik_flows.flow, [slug, default-provider-invalidation-flow]]
        invalidate_sessions_on_logout: true
        basic_auth_enabled: false

    - model: authentik_core.application
      state: present
      identifiers:
        slug: ${yamlString host.slug}
      attrs:
        name: ${yamlString host.slug}
        provider: !KeyOf ${yamlString "${host.slug}-provider"}
        meta_launch_url: ${yamlString "${host.host}/"}
  '';

  outpostProviders = lib.concatMapStringsSep "\n"
    (host: "        - !KeyOf ${yamlString "${host.slug}-provider"}")
    forwardAuthHosts;

  redirectUriEntries = app: lib.concatMapStringsSep "\n" (uri:
    "      - matching_mode: strict\n"
    + "        url: ${yamlString uri}\n"
    + "        redirect_uri_type: authorization"
  ) app.redirectUris;

  oidcEntry = app: ''
    - model: authentik_providers_oauth2.oauth2provider
      id: ${yamlString "${app.slug}-oidc-provider"}
      state: present
      identifiers:
        name: ${yamlString app.name}
      attrs:
        client_type: confidential
        client_id: ${yamlString app.clientId}
        client_secret: !File ${yamlString (toString app.clientSecretFile)}
        authorization_flow: !Find [authentik_flows.flow, [slug, default-provider-authorization-explicit-consent]]
        invalidation_flow: !Find [authentik_flows.flow, [slug, default-provider-invalidation-flow]]
        redirect_uris:
    ${redirectUriEntries app}
        grant_types:
          - authorization_code
          - refresh_token
        access_code_validity: minutes=1
        access_token_validity: minutes=5
        refresh_token_validity: days=30
        sub_mode: hashed_user_id

    - model: authentik_core.application
      state: present
      identifiers:
        slug: ${yamlString app.slug}
      attrs:
        name: ${yamlString app.name}
        provider: !KeyOf ${yamlString "${app.slug}-oidc-provider"}
        meta_launch_url: ${yamlString app.launchUrl}
  '';

  applicationsBlueprint = builtins.toFile "lattice-authentik-applications.yaml" ''
    version: 1
    metadata:
      name: Lattice - Authentik applications
      labels:
        blueprints.goauthentik.io/instantiate: "true"
    entries:
      # Apply packaged dependencies explicitly: blueprint discovery order is
      # intentionally unspecified by Authentik.
      - model: authentik_blueprints.metaapplyblueprint
        attrs:
          identifiers:
            name: Default - Provider authorization flow (explicit consent)
          required: true
      - model: authentik_blueprints.metaapplyblueprint
        attrs:
          identifiers:
            name: Default - Provider invalidation flow
          required: true

    ${indent "  " (lib.concatMapStringsSep "\n" providerEntry forwardAuthHosts)}

    ${indent "  " (lib.concatMapStringsSep "\n" oidcEntry oidcApplications)}

    ${lib.optionalString (cfg.forwardAuth != [ ]) ''
      - model: authentik_outposts.outpost
        state: present
        identifiers:
          name: authentik Embedded Outpost
        attrs:
          providers:
    ${outpostProviders}
    ''}
  '';

  # Expose the generated file to Authentik's native discovery/reconciliation
  # while retaining every stock blueprint from the private Python environment
  # embedded in nixpkgs's `ak` wrapper. Authentik rejects blueprint paths whose
  # realpath escapes AUTHENTIK_BLUEPRINTS_DIR, so these must be real copies:
  # symlinkJoin/cp -s makes even packaged blueprints fail as "Invalid blueprint
  # path" when apply_blueprint resolves the link into another store path.
  blueprintsDir = pkgs.runCommand "authentik-blueprints" { } ''
    set -euo pipefail
    ak=${lib.escapeShellArg ak}
    python_root="$(${pkgs.gnused}/bin/sed -n \
      "s|^PATH='\(/nix/store/[^']*\)/bin'\$PATH.*|\1|p" "$ak" | \
      ${pkgs.gnused}/bin/sed -n '1p')"
    if [[ ! -d "$python_root/blueprints" ]]; then
      echo "authentik-blueprints: could not resolve packaged blueprints from $ak" >&2
      exit 1
    fi
    mkdir -p "$out"
    ${pkgs.coreutils}/bin/cp -rL "$python_root/blueprints/." "$out/"
    mkdir -p "$out/lattice"
    ${pkgs.coreutils}/bin/cp ${applicationsBlueprint} "$out/lattice/applications.yaml"
  '';

  applicationsBlueprintPath = "${blueprintsDir}/lattice/applications.yaml";
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
      {
        assertion = lib.all (app: lib.hasPrefix "/" app.callbackPath) cfg.oidcApplications;
        message = "lattice.authentik: every oidcApplications callbackPath must start with `/`.";
      }
      {
        assertion = lib.length (lib.unique (map (app: app.service) cfg.oidcApplications))
          == lib.length cfg.oidcApplications;
        message = "lattice.authentik: oidcApplications service names must be unique.";
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
        ExecStart = migrate;
      };
      environment = commonEnv;
    };

    systemd.services.authentik-server = {
      description = "Authentik SSO server (loopback)";
      wantedBy = [ "multi-user.target" ];
      requires = [ "authentik-migrate.service" ]
        ++ lib.optional blueprintEnabled "authentik-applications-blueprint.service";
      after = [ "authentik-migrate.service" "network-online.target" ]
        ++ lib.optional blueprintEnabled "authentik-applications-blueprint.service";
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
      requires = [ "authentik-migrate.service" ]
        ++ lib.optional blueprintEnabled "authentik-applications-blueprint.service";
      after = [ "authentik-migrate.service" "network-online.target" ]
        ++ lib.optional blueprintEnabled "authentik-applications-blueprint.service";
      wants = [ "network-online.target" ];
      # Worker env, not commonEnv: see workerEnv above — the Rust worker needs
      # valid, distinct loopback listeners (empty would crash the config parse,
      # and 9220 would collide with the server).
      environment = workerEnv;
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

    # Apply the native Authentik Blueprint transactionally after migrations and
    # before server/worker/Caddy. The worker then discovers the same file in
    # AUTHENTIK_BLUEPRINTS_DIR and keeps applying it on Authentik's normal
    # reconciliation schedule.
    systemd.services.authentik-applications-blueprint = lib.mkIf blueprintEnabled {
      description = "Apply Authentik applications blueprint";
      wantedBy = [ "multi-user.target" ];
      requires = [ "authentik-migrate.service" ];
      after = [ "authentik-migrate.service" ];
      before = [ "authentik-server.service" "authentik-worker.service" "caddy.service" ];
      environment = commonEnv // {
        # Non-secret path exposed for evaluation/build-time contract tests.
        LATTICE_AUTHENTIK_APPLICATIONS_BLUEPRINT = applicationsBlueprintPath;
        LATTICE_AUTHENTIK_APPLICATIONS_BLUEPRINT_SOURCE = toString applicationsBlueprint;
      };
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        User = cfg.dbUser;
        Group = cfg.dbUser;
        WorkingDirectory = dataDir;
        EnvironmentFile = envFiles;
        # Apply dependencies explicitly as well as referencing them through
        # metaapplyblueprint. This recovers cleanly if a previous broken
        # deployment never instantiated one of the stock flow blueprints.
        ExecStart = [
          "${ak} apply_blueprint ${blueprintsDir}/default/flow-default-provider-authorization-explicit-consent.yaml"
          "${ak} apply_blueprint ${blueprintsDir}/default/flow-default-provider-invalidation.yaml"
          "${ak} apply_blueprint ${applicationsBlueprintPath}"
        ];
      };
    };

    systemd.services.caddy = lib.mkIf blueprintEnabled {
      requires = [ "authentik-applications-blueprint.service" ];
      after = [ "authentik-applications-blueprint.service" ];
    };

    # Authentik's loopback port is intentionally NOT added to the firewall: the
    # single external ingress is Caddy (profiles/tcp-gateway), which proxies to
    # Authentik over loopback without a rule (same contract as Grafana, F12).
  };
}
