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
      };
    };
  };
}
