{ config, lib, pkgs, ... }:

let
  service = "telegram-gateway";
  dbService = "telegram-gateway-db";
  port = (import ../../profiles/networking/ports.nix).telegram-gateway;
  sessionDir = "/var/lib/${service}/account";
  pg = pkgs.postgresql_17;
  pgData = "/var/lib/${dbService}/${pg.psqlSchema}";
  pgSocket = "/run/${dbService}";
  secrets = config.age.secrets;
in
{
  age.secrets = {
    telegram-gateway-session.file = ./secrets/telegram-gateway-session.age;
    telegram-gateway-env.file = ./secrets/telegram-gateway-env.age;
    telegram-gateway-access.file = ./secrets/telegram-gateway-access.age;
  };

  users.groups.${service} = { };
  users.users = {
    ${service} = { isSystemUser = true; group = service; };
    ${dbService} = { isSystemUser = true; group = service; };
  };

  environment.persistence."/persist".directories = [
    { directory = "/var/lib/${service}"; user = service; group = service; mode = "0700"; }
    { directory = "/var/lib/${dbService}"; user = dbService; group = service; mode = "0700"; }
  ];

  # A separate cluster keeps the gateway journal persisted independently of Authentik.
  systemd.services.${dbService} = {
    description = "PostgreSQL for Telegram Gateway";
    environment.PGDATA = pgData;
    preStart = ''
      if [ ! -f "$PGDATA/PG_VERSION" ]; then
        ${pg}/bin/initdb --auth-local=peer --auth-host=reject --no-instructions >/dev/null
      fi
    '';
    postStart = ''
      ${pg}/bin/psql -X -v ON_ERROR_STOP=1 -h ${pgSocket} -d postgres <<'SQL'
      SELECT 'CREATE ROLE "telegram-gateway" LOGIN'
      WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'telegram-gateway') \gexec
      SELECT 'CREATE DATABASE "telegram-gateway" OWNER "telegram-gateway"'
      WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'telegram-gateway') \gexec
      SQL
    '';
    serviceConfig = {
      Type = "notify";
      User = dbService;
      Group = service;
      RuntimeDirectory = dbService;
      RuntimeDirectoryMode = "0750";
      StateDirectory = dbService;
      StateDirectoryMode = "0700";
      ExecStart = "${pg}/bin/postgres -D ${pgData} -k ${pgSocket} -c listen_addresses= -c unix_socket_permissions=0770";
      KillSignal = "SIGINT";
      Restart = "on-failure";
      RestartSec = 5;
    };
  };

  systemd.services.${service} = {
    description = "Telegram Gateway";
    wantedBy = [ "multi-user.target" ];
    wants = [ "network-online.target" ];
    requires = [ "${dbService}.service" ];
    after = [ "network-online.target" "${dbService}.service" ];
    partOf = [ "${dbService}.service" ];
    restartTriggers = [
      secrets.telegram-gateway-env.file
      secrets.telegram-gateway-access.file
    ];
    environment = {
      DATABASE_URL = "host=${pgSocket} dbname=${service} user=${service} sslmode=disable";
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

  lattice.tcp-gateway = {
    telegramGatewayPort = port;
    meshExclude = [ service ];
  };
}
