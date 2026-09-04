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
      base = removeAttrs iface [ "address" "forward_address" "extraConfig" "openFirewall" ];
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

  tcpTypes = [ "TCPClientInterface" "TCPServerInterface" ];
  tcpFields = [ "type" "enabled" "target_host" "target_port" "listen_ip" "listen_port" "max_connections" "openFirewall" ];
  interfaceAssertions = name: iface: [
    {
      assertion = lib.intersectLists tcpFields (builtins.attrNames iface.extraConfig) == [ ];
      message = "lattice.rns-server.interfaces.${name}: use typed options instead of extraConfig for TCP connection fields, type, enabled and openFirewall.";
    }
    {
      assertion = !iface.enabled || iface.type != "TCPClientInterface"
        || (iface.target_host != null && iface.target_port != null);
      message = "lattice.rns-server.interfaces.${name}: an enabled TCPClientInterface requires target_host and target_port.";
    }
    {
      assertion = !iface.enabled || iface.type != "TCPServerInterface"
        || (iface.listen_ip != null && iface.listen_port != null);
      message = "lattice.rns-server.interfaces.${name}: an enabled TCPServerInterface requires listen_ip and listen_port.";
    }
    {
      assertion = !iface.enabled || !(builtins.elem iface.type tcpTypes)
        || (iface.address == null && iface.port == null);
      message = "lattice.rns-server.interfaces.${name}: TCP uses target_host/target_port or listen_ip/listen_port; address and port aliases are not supported.";
    }
    {
      assertion = !iface.openFirewall || iface.type == "TCPServerInterface";
      message = "lattice.rns-server.interfaces.${name}: openFirewall is only supported for TCPServerInterface.";
    }
  ];
in
{
  config = lib.mkIf cfg.enable {
    lattice.rns-server.configFile = rnsConfig;

    assertions = lib.concatLists (lib.mapAttrsToList interfaceAssertions cfg.interfaces);

    networking.firewall.allowedTCPPorts = lib.unique (lib.filter (port: port != null)
      (map (iface: iface.listen_port) (lib.filter
        (iface: iface.enabled && iface.type == "TCPServerInterface" && iface.openFirewall)
        (builtins.attrValues cfg.interfaces))));

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
        UMask = "0077";
        WorkingDirectory = toString cfg.configDir;
      };
    };
  };
}
