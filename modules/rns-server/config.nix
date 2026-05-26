{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.rns-server;

  renderValue = value:
    if builtins.isBool value then (if value then "Yes" else "No")
    else if builtins.isList value then lib.concatStringsSep ", " (map renderValue value)
    else toString value;

  cleanAttrs = attrs: lib.filterAttrs (_: value: value != null && value != [ ]) attrs;

  renderSection = name: attrs:
    let
      cleaned = cleanAttrs attrs;
      lines = lib.mapAttrsToList (key: value: "${key} = ${renderValue value}") cleaned;
    in
    lib.optionalString (cleaned != { }) ("[${name}]\n" + lib.concatStringsSep "\n" lines + "\n\n");

  splitAddress = option: value:
    let
      match = builtins.match "(.+):([0-9]+)" value;
    in
    if match == null then throw "lattice.rns-server.interfaces.<name>.${option} must be in host:port form"
    else { host = builtins.elemAt match 0; port = builtins.fromJSON (builtins.elemAt match 1); };

  reticulumConfig = cleanAttrs (cfg.reticulum // {
    remote_management_allowed = cfg.reticulum.remote_management_allowed;
  });

  interfaceConfig = iface:
    let
      listen = if iface.address == null then { } else splitAddress "address" iface.address;
      forward = if iface.forward_address == null then { } else splitAddress "forward_address" iface.forward_address;
      base = removeAttrs iface [ "address" "forward_address" "extraConfig" ];
    in
    cleanAttrs (base // iface.extraConfig // lib.optionalAttrs (listen != { }) {
      listen_ip = if iface.listen_ip == null then listen.host else iface.listen_ip;
      listen_port = if iface.listen_port == null then listen.port else iface.listen_port;
    } // lib.optionalAttrs (forward != { }) {
      forward_ip = if iface.forward_ip == null then forward.host else iface.forward_ip;
      forward_port = if iface.forward_port == null then forward.port else iface.forward_port;
    });

  renderInterface = name: iface:
    let
      lines = lib.mapAttrsToList (key: value: "    ${key} = ${renderValue value}") (interfaceConfig iface);
    in
    "  [[${name}]]\n" + lib.concatStringsSep "\n" lines + "\n";

  rnsConfig = pkgs.writeText "rns-config" (
    renderSection "reticulum" reticulumConfig
    + renderSection "logging" { inherit (cfg.logging) loglevel; }
    + "[interfaces]\n"
    + lib.concatStringsSep "\n" (lib.mapAttrsToList renderInterface cfg.interfaces)
  );

  serverConfig = pkgs.writeText "rns-server.json" (builtins.toJSON {
    stats_db_path = cfg.server.statsDbPath;
    rnsd_bin = cfg.server.rnsdBin;
    sentineld_bin = cfg.server.sentineldBin;
    statsd_bin = cfg.server.statsdBin;
    http = {
      enabled = cfg.server.http.enabled;
      host = cfg.server.http.host;
      port = cfg.server.http.port;
      auth_token = cfg.server.http.authToken;
      disable_auth = cfg.server.http.disableAuth;
    };
  });

  startArgs = [ "start" "--config" (toString cfg.configDir) ] ++ cfg.extraArgs;
in
{
  config = lib.mkIf cfg.enable {
    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = toString cfg.configDir;
    };

    systemd.services.rns-server = {
      description = "Reticulum rns-server";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      path = [ cfg.package ];
      preStart = ''
        install -d -m 0750 -o ${lib.escapeShellArg cfg.user} -g ${lib.escapeShellArg cfg.group} ${lib.escapeShellArg (toString cfg.configDir)}
        install -m 0640 -o ${lib.escapeShellArg cfg.user} -g ${lib.escapeShellArg cfg.group} ${rnsConfig} ${lib.escapeShellArg (toString cfg.configDir)}/config
        install -m 0640 -o ${lib.escapeShellArg cfg.user} -g ${lib.escapeShellArg cfg.group} ${serverConfig} ${lib.escapeShellArg (toString cfg.configDir)}/rns-server.json
      '';
      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        ExecStart = "${lib.getExe cfg.package} ${lib.escapeShellArgs startArgs}";
        Restart = "on-failure";
        RestartSec = 5;
        WorkingDirectory = toString cfg.configDir;
      };
    };
  };
}
