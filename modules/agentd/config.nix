{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.agentd;
  stateDir = "/var/lib/${cfg.stateDirectory}";
  identityPath = if cfg.identityFile != null then cfg.identityFile else "${stateDir}/identity";
  workDirPath = if cfg.workDir != null then cfg.workDir else "${stateDir}/work";

  args = [
    "--identity-file" identityPath
    "--agent-subject" cfg.agentSubject
    "--work-dir" workDirPath
  ];

  execScript = pkgs.writeShellScript "agentd-start" ''
    set -eu
    key_file=""
    if [ -n "''${CREDENTIALS_DIRECTORY-}" ] && [ -f "$CREDENTIALS_DIRECTORY/realm-key" ]; then
      key_file="$CREDENTIALS_DIRECTORY/realm-key"
    elif [ -n "${toString cfg.realmKeyFile}" ] && [ -f "${toString cfg.realmKeyFile}" ]; then
      key_file="${toString cfg.realmKeyFile}"
    fi

    if [ -z "$key_file" ]; then
      echo "agentd: realm key file is required" >&2
      exit 1
    fi

    mkdir -p "${workDirPath}"
    exec ${lib.getExe cfg.package} --realm-key-file "$key_file" ${lib.escapeShellArgs args}
  '';
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.realmKeyFile != null;
        message = "lattice.agentd: realmKeyFile must be set.";
      }
    ];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = stateDir;
      createHome = false;
    };

    systemd.services.lattice-agentd = {
      description = "Lattice agentd agent execution daemon";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" "rns-server.service" ];
      wants = [ "rns-server.service" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = cfg.stateDirectory;
        StateDirectoryMode = "0700";
        Restart = "always";
        RestartSec = "5s";

        LoadCredential = lib.optional (cfg.realmKeyFile != null) "realm-key:${toString cfg.realmKeyFile}";

        ExecStart = "${execScript}";

        # Security hardening
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        ProtectControlGroups = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        LockPersonality = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
      };
    };
  };
}
