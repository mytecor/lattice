{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.git-watchd;
  stateDir = "/var/lib/${cfg.stateDirectory}";
  identityPath = if cfg.identityFile != null then cfg.identityFile else "${stateDir}/identity";

  args = [
    "--identity-file" identityPath
    "--head-subject" cfg.headSubject
    "--watch-subject" cfg.watchSubject
    "--poll-interval" cfg.pollInterval
  ]
  ++ lib.optionals (cfg.allowedDir != null) [ "--allowed-dir" cfg.allowedDir ];

  # Shell wrapper to handle realm key loading via credentials
  execScript = pkgs.writeShellScript "git-watchd-start" ''
    set -eu
    key_file=""
    if [ -n "''${CREDENTIALS_DIRECTORY-}" ] && [ -f "$CREDENTIALS_DIRECTORY/realm-key" ]; then
      key_file="$CREDENTIALS_DIRECTORY/realm-key"
    elif [ -n "${toString cfg.realmKeyFile}" ] && [ -f "${toString cfg.realmKeyFile}" ]; then
      key_file="${toString cfg.realmKeyFile}"
    fi

    if [ -z "$key_file" ]; then
      echo "git-watchd: realm key file is required" >&2
      exit 1
    fi

    exec ${lib.getExe cfg.package} --realm-key-file "$key_file" ${lib.escapeShellArgs args}
  '';
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.realmKeyFile != null;
        message = "lattice.git-watchd: realmKeyFile must be set.";
      }
    ];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = stateDir;
      createHome = false;
    };

    systemd.services.lattice-git-watchd = {
      description = "Lattice git-watchd Git revision watcher daemon";
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
