{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.foxbridge-camoufox;
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
      # Foxbridge launches Camoufox as a child; it needs no shell login.
      shell = "${pkgs.bash}/bin/bash";
      home = "/var/empty";
    };

    systemd.tmpfiles.rules = [
      "d ${toString cfg.camoufox.profileDir} 0700 ${cfg.user} ${cfg.group} - -"
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
        # Disposable profile lives on tmpfs; recreated by tmpfiles on boot.
        RuntimeDirectory = "foxbridge-camoufox";
        RuntimeDirectoryMode = "0700";
        WorkingDirectory = "/run/foxbridge-camoufox";

        # f18-08: drop the fragile f18-07 LD_LIBRARY_PATH glob — the Nix
        # camoufox derivation is auto-patched, so the binary self-contains its
        # store deps. ExecStart runs Foxbridge directly.
        ExecStart = lib.concatStringsSep " " [
          (lib.getExe cfg.package)
          "--port ${toString cfg.port}"
          "--binary ${lib.getExe cfg.camoufoxPackage}"
          "--headless"
          "--profile ${toString cfg.camoufox.profileDir}"
        ];
        # f18-07: Camoufox content-processes under a strict systemd sandbox
        # SEGV (forkserver coredump → Juggler never delivers frameId). Disable
        # the content sandbox — a headless anti-detect browser in the homelab,
        # fingerprint is verified separately (f18-05).
        Environment = [
          "HOME=/run/foxbridge-camoufox"
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

        # Hardening: the browser runtime reaches the network for the agent's
        # tasks. No privileged caps, no elevation; profile is a tmpfs dir.
        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
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
        ReadWritePaths = [ "/run/foxbridge-camoufox" ];
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
