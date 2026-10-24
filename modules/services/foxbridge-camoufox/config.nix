{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.foxbridge-camoufox;
  browserCommand = "${lib.getExe cfg.package} --port ${toString cfg.port} --binary ${lib.getExe cfg.camoufoxPackage} --headless";
in
{
  config = lib.mkIf cfg.enable {
    # F18: the CDP endpoint is loopback-only by construction (Foxbridge binds
    # 127.0.0.1 and has no host flag). This assertion makes the security
    # contract explicit — refusing any config that claims otherwise — instead
    # of silently ignoring a non-loopback listenAddress.
    assertions = [{
      assertion = cfg.listenAddress == "127.0.0.1"
        || cfg.listenAddress == "::1"
        || cfg.listenAddress == "localhost";
      message = ''
        lattice.foxbridge-camoufox: listenAddress must stay on loopback
        (127.0.0.1 / ::1 / localhost). Foxbridge binds 127.0.0.1 by
        construction and exposes no flag to change it; the CDP endpoint is
        never published outside the host (f18-07 verified externally-closed).
      '';
    }];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      extraGroups = [ "render" "video" ];
      # Foxbridge launches Camoufox as a child; it needs no shell login.
      shell = "${pkgs.bash}/bin/bash";
      home = "/var/empty";
    };

    # Persistent home for the browser: Camoufox creates its profile/cache
    # under $HOME/.cache/camoufox. This must be a REAL (non-tmpfs) directory —
    # f18-08 node validation: HOME on the /run tmpfs (RuntimeDirectory)
    # crashes under the full systemd hardening (Juggler Browser.enable never
    # completes), while HOME on /var/lib works with the exact same hardening.
    systemd.tmpfiles.rules = [
      "d ${cfg.stateDir} 0700 ${cfg.user} ${cfg.group} - -"
    ];

    systemd.services.foxbridge-camoufox = {
      description = "Foxbridge CDP compatibility layer for Camoufox (F18 browser runtime)";
      documentation = [ "https://github.com/VulpineOS/foxbridge" ];
      # Long-running browser runtime; lives independently of Jev. No consumer
      # starts or stops it.
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        # Persistent home on /var/lib, NOT /run tmpfs (see tmpfiles note).
        WorkingDirectory = cfg.stateDir;

        ExecStart = browserCommand;

        # f18-07: Camoufox content-processes under a strict systemd sandbox
        # SEGV (forkserver coredump → Juggler never delivers frameId). Disable
        # the content sandbox — a headless anti-detect browser in the homelab,
        # fingerprint is verified separately (f18-05).
        Environment = [
          "HOME=${cfg.stateDir}"
          "MOZ_DISABLE_CONTENT_SANDBOX=1"
        ] ++ lib.optionals cfg.camoufox.humanize [
          # f18-06: humanize is delivered to the browser as an env config
          # chunk. A tiny {"humanize":true} fits CAMOU_CONFIG_1 alone.
          "CAMOU_CONFIG_1=${builtins.toJSON { humanize = true; }}"
        ];

        Restart = "on-failure";
        RestartSec = 3;
        TimeoutStartSec = 60;
        KillMode = "mixed";
        GuessMainPID = true;

        # GPU acceleration: access to render node for EGL / WebRender and VA-API
        DevicePolicy = "closed";
        DeviceAllow = [
          "/dev/dri/renderD128 rw"
          "/dev/dri/card0 rw"
        ];
        PrivateDevices = false;

        # Hardening: the browser runtime reaches the network for the agent's
        # tasks. No privileged caps or elevation; profile stays in stateDir.
        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
        NoNewPrivileges = true;
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
        ReadWritePaths = [ cfg.stateDir ];
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
        ];
      };
    };
  };
}
