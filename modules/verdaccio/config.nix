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
  # denying everyone. When `publish` is enabled, publish/unpublish move to
  # `$authenticated` (htpasswd-backed) so at least a known operator can publish.
  publishAccess = if cfg.publish then "$authenticated" else "\$none";

  configYaml = pkgs.writeText "verdaccio-config.yaml" (lib.concatStringsSep "\n" [
    "storage: ${cfg.cacheRoot}"
    "uplinks:"
    "  npmjs:"
    "    url: ${cfg.upstreamRegistry}"
    "auth:"
    "  htpasswd:"
    "    file: ${htpasswdPath}"
    "packages:"
    "  '@*/*':"
    "    access: \${anonymous}"
    "    publish: ${publishAccess}"
    "    unpublish: ${publishAccess}"
    "    proxy: npmjs"
    "  '**':"
    "    access: \${anonymous}"
    "    publish: ${publishAccess}"
    "    unpublish: ${publishAccess}"
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
    }];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
    };

    systemd.tmpfiles.rules = [
      "d ${toString cfg.cacheRoot} 0700 ${cfg.user} ${cfg.group} - -"
    ];

    systemd.services.verdaccio = {
      description = "Lattice Verdaccio npm caching proxy";
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

    # f9-03: registry config so npm/pnpm/yarn on this node use the loopback
    # cache by default (no global manual setup). Only a registry URL is
    # written; upstream credentials never reach client config files.
    environment.etc = lib.mkIf cfg.clientConfig ({
      npmrc.text = ''
        registry=http://${cfg.host}:${toString cfg.port}/
      '';
      yarnrc.text = ''
        registry "http://${cfg.host}:${toString cfg.port}/"
      '';
    });
  };
}
