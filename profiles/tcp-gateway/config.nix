{ config, lib, ... }:

let
  domain = if config.networking.domain != null && config.networking.domain != ""
           then config.networking.domain
           else "lattice";

  hostName = config.networking.hostName;

  # Автоматически собираем проксирование для активных TCP/HTTP сервисов
  proxiedServices = lib.mkMerge [
    # 1. Radicle HTTP Gateway
    (lib.mkIf (config.services.radicle.enable or false && config.services.radicle.httpd.enable or false) {
      "radicle.${hostName}.${domain}" = {
        extraConfig = ''
          reverse_proxy 127.0.0.1:${toString config.services.radicle.httpd.listenPort}
        '';
      };
    })

    # 2. RNS Server HTTP Control Plane
    (lib.mkIf (
      (config.lattice.rns-server.enable or false) &&
      (config.lattice.rns-server.server.http.enabled or false) &&
      (config.lattice.rns-server.server.http.port or null != null)
    ) {
      "rns-server.${hostName}.${domain}" = {
        extraConfig = ''
          reverse_proxy 127.0.0.1:${toString config.lattice.rns-server.server.http.port}
        '';
      };
    })
  ];
in
{
  config.services.caddy = {
    enable = true;
    virtualHosts = proxiedServices;
  };

  networking.firewall.allowedTCPPorts = [ 80 443 ];
}
