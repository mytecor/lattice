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

        # f18-08: drop the fragile f18-07 LD_LIBRARY_PATH glob — the Nix
        # camoufox derivation is auto-patched, so the binary self-contains its
        # store deps. ExecStart runs Foxbridge directly.
        #
        # No --profile flag: Camoufox derives its default profile from
        # $HOME/.cache/camoufox. A custom --profile path is fatal — f18-08
        # node validation: pointing it at the /run tmpfs (wiped by
        # RuntimeDirectory on every start) or anywhere else makes the Juggler
        # Browser.enable handshake stall (timeout / client closed).
        ExecStart = lib.concatStringsSep " " [
          (lib.getExe cfg.package)
          "--port ${toString cfg.port}"
          "--binary ${lib.getExe cfg.camoufoxPackage}"
          "--headless"
        ];
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
        ReadWritePaths = [ cfg.stateDir ];
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
        # f18-08 validation: ~@resources must NOT reach Firefox. Camoufox
        # calls setpriority (syscall 141) on startup; the seccomp filter then
        # kills the process with SIGSYS/31, and foxbridge-camoufox endlessly
        # crash-cycles (dmesg: sig=31 syscall=141, Restart=on-failure). The
        # f18-07 manual PoC ran un-sandboxed so this never surfaced. Kept:
        # @system-service allow-list + ~@privileged + the full capability/
        # namespaces/fs hardening. Same tradeoff class as verdaccio dropping
        # MemoryDenyWriteExecute for V8 (see modules/verdaccio/README.md).
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
        ];
      };
    };
  };
}
