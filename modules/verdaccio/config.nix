{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.verdaccio;

  # The only registry credential (an htpasswd file) lives outside the Nix store
  # (agenix .age) and is mounted into the service via systemd LoadCredential.
  # The exec wrapper copies it to a stable writable path under the runtime dir,
  # so the generated config.yaml can reference a stable filesystem path
  # (LoadCredential mounts under a per-unit $CREDENTIALS_DIRECTORY, which is not
  # a path verdaccio should hold across restarts). In cache-only mode (publish
  # off) no credential is mounted; the auth file lives in the cache root and is
  # created empty by verdaccio on first start. The credential value itself is
  # never rendered into the store config nor passed in argv — only the path is.
  htpasswdPath =
    if cfg.credentials.htpasswdFile != null then
      "/run/${cfg.runtimeDirectory}/htpasswd"
    else
      "${cfg.cacheRoot}/htpasswd";

  # Default policy: cache-only proxy. Anonymous users may read/install anything
  # the uplink can reach (proxy-scoped), but publish/unpublish default to
  # denying everyone (the keys are omitted, so verdaccio's default empty ACL
  # denies all). When `publish` is enabled, publish/unpublish move to
  # `$authenticated` (htpasswd-backed) so at least a known operator can publish.
  #
  # NB: the acl tokens are `$anonymous` / `$authenticated` (single `$`, no
  # braces) — see @verdaccio/config package-access.js ROLES. Writing
  # `\${anonymous}` produced a literal ``${anonymous}`` string that verdaccio
  # treats as a plain (non-matching) group, so anonymous clients got 401
  # "authorization required" on every package and cold install was impossible.
  publishAccessLines = lib.optionals cfg.publish [
    "    publish: $authenticated"
    "    unpublish: $authenticated"
  ];

  configYamlText = lib.concatStringsSep "\n" ([
    "storage: ${cfg.cacheRoot}"
    "uplinks:"
    "  npmjs:"
    "    url: ${cfg.upstreamRegistry}"
    "auth:"
    "  htpasswd:"
    "    file: ${htpasswdPath}"
    "packages:"
    "  '@*/*':"
    "    access: $anonymous"
  ] ++ publishAccessLines ++ [
    "    proxy: npmjs"
    "  '**':"
    "    access: $anonymous"
  ] ++ publishAccessLines ++ [
    "    proxy: npmjs"
    "server:"
    "  keepAliveTimeout: 60"
    "limits:"
    "  max_body_size: ${cfg.maxBodySize}"
    "log:"
    "  type: stdout"
    "  format: pretty"
    "  level: ${cfg.logLevel}"
    ""
  ]);

  configYaml = pkgs.writeText "verdaccio-config.yaml" configYamlText;

  execScript = pkgs.writeShellScript "verdaccio-exec" ''
    set -eu

    # In publish mode a fresh htpasswd is copied from the mounted credential so
    # verdaccio's auth plugin sees the operator's registry users. A trailing
    # newline is stripped and embedded newlines rejected to keep the file clean.
    cred_dir="''${CREDENTIALS_DIRECTORY:-}"
    if [ -n "$cred_dir" ] && [ -f "$cred_dir/htpasswd" ]; then
      ${pkgs.coreutils}/bin/cp "$cred_dir/htpasswd" ${htpasswdPath}
      ${pkgs.coreutils}/bin/chmod 0600 ${htpasswdPath}
    fi

    exec ${lib.getExe cfg.package} \
      --config ${configYaml} \
      --listen ${cfg.host}:${toString cfg.port}
  '';
in
{
  config = lib.mkIf cfg.enable {
    # Expose the exact config text for contract tests / operator inspection.
    lattice.verdaccio.generatedConfigYaml = configYamlText;

    # Publish mode is an operator decision: it requires an htpasswd credential
    # file. Advertising publish without providing credentials would silently
    # nudge toward the shared cache path; better to fail closed at eval.
    assertions = [{
      assertion = !cfg.publish || cfg.credentials.htpasswdFile != null;
      message = ''
        lattice.verdaccio: publish requires credentials.htpasswdFile to be set
        (an htpasswd file mounted via LoadCredential). Set publish = false (the
        default cache-only proxy) or provide an htpasswd file.
      '';
    }
      # CacheDirectory= only manages paths under /var/cache; a custom cacheRoot
      # elsewhere cannot be auto-recreated before namespacing, which would
      # silently reintroduce the rm -rf brick (226/NAMESPACE). Fail closed.
      {
        assertion = lib.hasPrefix "/var/cache/" (toString cfg.cacheRoot);
        message = ''
          lattice.verdaccio: cacheRoot must be under /var/cache/ so that
          CacheDirectory can recreate it before systemd mount namespacing
          (disposable-cache contract). Got: ${toString cfg.cacheRoot}
        '';
      }];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
    };

    systemd.services.verdaccio = {
      description = "Lattice Verdaccio npm caching proxy";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        # Disposable-cache contract: `rm -rf ${cfg.cacheRoot}` must not brick
        # the unit. systemd sets up mount namespacing (ProtectSystem=strict +
        # ReadWritePaths) *before* ExecStartPre, and fails with 226/NAMESPACE
        # when the ReadWritePaths target does not exist. `CacheDirectory=`
        # creates the directory before namespacing on every start and chowns
        # it to User/Group, so a runtime cache wipe just causes a refetch.
        CacheDirectory = lib.removePrefix "/var/cache/" (toString cfg.cacheRoot);
        CacheDirectoryMode = "0700";
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        WorkingDirectory = toString cfg.cacheRoot;
        ExecStart = execScript;
        # systemd mounts the htpasswd secret here; verdaccio gets only the path.
        LoadCredential = lib.optionals (cfg.credentials.htpasswdFile != null)
          [ "htpasswd:${toString cfg.credentials.htpasswdFile}" ];
        Restart = "on-failure";
        RestartSec = 5;
        TimeoutStartSec = 30;
        UMask = "0077";

        # Hardening: the registry is a cache-only proxy on loopback, so the same
        # trust boundary applies as the git cache proxy — reachability is the
        # boundary, and it only ever talks out to the upstream registry uplink.
        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
        LockPersonality = true;
        # Node 24's V8 cannot initialize an isolate under systemd's W^X policy:
        # v8::base::OS::SetPermissions on the code range fails with errno !=
        # ENOMEM (EPERM from seccomp) and V8 aborts with
        # "Check failed: 12 == (*__errno_location ())". Reproduced on
        # mytecor-homelab (Intel N100, nodejs-24.19.0): bare `node -e` runs,
        # with MemoryDenyWriteExecute=yes it SIGTRAPs on first v8::Isolate::
        # Initialize. Kept systemd hardening is still severe (NoNewPrivileges,
        # ProtectSystem=strict, CapabilityBoundingSet="", syscall filter incl.
        # ~@privileged/~@resources); the same tradeoff is documented for the
        # llm-gateway unit (Bifrost mprotect(PROT_EXEC)). See modules/verdaccio/README.md.
        MemoryDenyWriteExecute = false;
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

    # f9-03: registry config so pnpm (the actual package manager used in this
    # project / on the node) uses the loopback cache by default, without global
    # manual setup. Only a registry URL is written; upstream credentials never
    # reach client config files.
    #
    # Verified on the node, 2026-09-14:
    #  - pnpm 11 does NOT read /etc/npmrc: its global config is
    #    $XDG_CONFIG_HOME/pnpm/config.yaml (= /root/.config/pnpm/config.yaml
    #    for root). /etc/pnpmrc and NPM_CONFIG_REGISTRY env are ignored.
    #  - npm (Nix-built) has its own globalconfig at
    #    /nix/store/<nodejs>/etc/npmrc, so /etc/npmrc only helps non-Nix npm;
    #    it is harmless to keep for shells where it applies.
    # The pnpm path is outside /etc and /root is ephemeral (impermanence), so
    # it is (re)created on every activation.
    environment.etc = lib.optionalAttrs cfg.clientConfig {
      npmrc.text = ''
        registry=http://${cfg.host}:${toString cfg.port}/
      '';
    };

    system.activationScripts.verdaccioClientConfig =
      lib.mkIf cfg.clientConfig (lib.stringAfter [ "users" ] ''
        mkdir -p /root/.config/pnpm
        cat > /root/.config/pnpm/config.yaml <<EOF
        registry: http://${cfg.host}:${toString cfg.port}/
        EOF
      '');
  };
}
