{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.pi-acp-daemon;
  stateDir = "/var/lib/${cfg.stateDirectory}";
  runtimeDir = "/run/${cfg.runtimeDirectory}";
  userHome = config.users.users.${cfg.user}.home or "/root";

  hydraConfig = pkgs.writeText "hydra-acp-config.json" (builtins.toJSON {
    daemon = {
      inherit (cfg) host port logLevel sessionIdleTimeoutSeconds;
      nonInteractiveOrphanTimeoutSeconds = 0;
      scrubEnv = [ ];
    };
    registry.pinned = true;
    agents.pi-acp = {
      command = lib.getExe cfg.agentPackage;
      args = [ ];
      env = {
        PI_ACP_DIR = "${stateDir}/pi-acp";
        PI_CODING_AGENT_DIR = "${userHome}/.pi/agent";
        # f8-06 fix: the spawned `pi --mode rpc` must see a shell and the Pi
        # tool contract. The daemon's own systemd PATH (NixOS service default)
        # has no `sh`, so without this Pi's bash tool fails with `spawn sh
        # ENOENT`. `daemon.scrubEnv = []` (above) lets this override reach the
        # agent verbatim.
        #
        # Prepend the declared tool profile (bash/git/tools, e.g.
        # `pkgs.lattice.pi-tool-profile`) and APPEND the NixOS system profile,
        # so node system tools such as `nix` stay available to the agent's
        # shell instead of being lost when the (tool-profile-only) PATH
        # replaces the daemon's.
        PATH = lib.makeBinPath cfg.path
          + ":/run/current-system/sw/bin"
          + ":/run/current-system/sw/sbin";
      };
    };
    defaultAgent = "pi-acp";
    defaultCwd = userHome;
    inherit (cfg) transformers defaultTransformers;
  });

  prepareRuntime = ''
    set -eu
    umask 077

    ${pkgs.coreutils}/bin/install -d -m 0700 \
      ${lib.escapeShellArg stateDir} ${lib.escapeShellArg runtimeDir}
    printf '%s\n' ${lib.escapeShellArg cfg.internalToken} \
      > ${lib.escapeShellArg "${runtimeDir}/auth-token"}
    ${pkgs.coreutils}/bin/chmod 0600 ${lib.escapeShellArg "${runtimeDir}/auth-token"}
    ${pkgs.coreutils}/bin/ln -sfn ${lib.escapeShellArg "${runtimeDir}/auth-token"} \
      ${lib.escapeShellArg "${stateDir}/auth-token"}
    ${pkgs.coreutils}/bin/ln -sfn ${hydraConfig} \
      ${lib.escapeShellArg "${stateDir}/config.json"}
  '';
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = builtins.hasAttr cfg.user config.users.users;
        message = "lattice.pi-acp-daemon.user must name an existing user.";
      }
      {
        assertion = builtins.hasAttr cfg.group config.users.groups;
        message = "lattice.pi-acp-daemon.group must name an existing group.";
      }
    ];

    lattice.pi-acp-daemon.generatedConfigFile = hydraConfig;

    environment.systemPackages = [ cfg.hydraPackage cfg.agentPackage ];

    systemd.services.pi-acp-daemon = {
      description = "Persistent multi-session Pi ACP daemon";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      environment = {
        HYDRA_ACP_HOME = stateDir;
        XDG_CONFIG_HOME = "${stateDir}/xdg-config";
      };

      preStart = prepareRuntime;

      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = cfg.stateDirectory;
        StateDirectoryMode = "0700";
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        RuntimeDirectoryPreserve = "restart";
        WorkingDirectory = userHome;
        ExecStart = lib.getExe' cfg.hydraPackage "hydra-acp-daemon";
        Restart = "on-failure";
        RestartSec = 5;
        UMask = "0077";

        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
        LockPersonality = true;
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
        # Pi must be able to edit workspaces under the service user's home.
        ProtectSystem = "full";
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
      } // lib.optionalAttrs cfg.privileged {
        # TEMPORARY privileged network/cap access for live diagnostics
        # (iw/ip/nl80211: open AF_NETLINK and grant CAP_NET_ADMIN so wireless
        # tooling works, plus let setuid/caps through for sudo). This is a
        # deliberate, documented STOPGAP — see the README "Temporary
        # privileged access" note. The strict defaults above are the target
        # posture; rework back to them once the diagnostics are done. Do not
        # enable on an untrusted LAN.
        AmbientCapabilities = [ "CAP_NET_ADMIN" "CAP_NET_RAW" "CAP_NET_BIND_SERVICE" "CAP_DAC_OVERRIDE" "CAP_SYS_ADMIN" "CAP_SETUID" "CAP_SETGID" ];
        CapabilityBoundingSet = [ "CAP_NET_ADMIN" "CAP_NET_RAW" "CAP_NET_BIND_SERVICE" "CAP_DAC_OVERRIDE" "CAP_SYS_ADMIN" "CAP_SETUID" "CAP_SETGID" ];
        NoNewPrivileges = false;
        PrivateDevices = false;
        ProtectSystem = false;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" "AF_NETLINK" ];
      };
    };
  };
}
