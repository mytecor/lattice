{ config, lib, pkgs, ... }:

let
  service = "telegram-gateway";
  port = (import ../../profiles/networking/ports.nix).telegram-gateway;
  sessionDir = "/var/lib/${service}/account";
  secrets = config.age.secrets;
in
{
  age.secrets = {
    telegram-gateway-session.file = ./secrets/telegram-gateway-session.age;
    telegram-gateway-env.file = ./secrets/telegram-gateway-env.age;
    telegram-gateway-access.file = ./secrets/telegram-gateway-access.age;
  };

  users.groups.${service} = { };
  users.users.${service} = {
    isSystemUser = true;
    group = service;
  };

  environment.persistence."/persist".directories = [
    { directory = "/var/lib/${service}"; user = service; group = service; mode = "0700"; }
  ];

  services.postgresql = {
    enable = true;
    ensureDatabases = [ service ];
    ensureUsers = [
      {
        name = service;
        ensureDBOwnership = true;
      }
    ];
  };

  systemd.services.${service} = {
    description = "Telegram Gateway";
    wantedBy = [ "multi-user.target" ];
    wants = [ "network-online.target" ];
    requires = [ "postgresql.service" ];
    after = [ "network-online.target" "postgresql.service" ];
    restartTriggers = [
      secrets.telegram-gateway-env.file
      secrets.telegram-gateway-access.file
    ];
    environment = {
      DATABASE_URL = "host=/run/postgresql dbname=${service} user=${service} sslmode=disable";
      TELEGRAM_LISTEN = "127.0.0.1:${toString port}";
      TELEGRAM_SESSION_DIR = sessionDir;
    };
    # gotd updates the session on disk. Seed an empty directory once.
    preStart = ''
      install -d -m 0700 ${sessionDir}
      if [ ! -e ${sessionDir}/session.json ]; then
        install -m 0600 "$CREDENTIALS_DIRECTORY/session.json" ${sessionDir}/.session-seed
        mv -T ${sessionDir}/.session-seed ${sessionDir}/session.json
      fi
    '';
    serviceConfig = {
      User = service;
      Group = service;
      StateDirectory = service;
      StateDirectoryMode = "0700";
      UMask = "0077";
      LoadCredential = [
        "session.json:${secrets.telegram-gateway-session.path}"
        "access.json:${secrets.telegram-gateway-access.path}"
      ];
      EnvironmentFile = secrets.telegram-gateway-env.path;
      ExecStart = "${lib.getExe pkgs.lattice.telegram-gateway} serve";
      Environment = "TELEGRAM_CONFIG=%d/access.json";
      NoNewPrivileges = true;
      ProtectSystem = "strict";
      ProtectHome = true;
      PrivateTmp = true;
      Restart = "on-failure";
      RestartSec = 5;
    };
  };

  lattice.tcp-gateway.telegramGatewayPort = port;
}
