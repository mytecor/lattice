{ config, ... }:

let
  domain =
    if config.networking.domain != null && config.networking.domain != ""
    then config.networking.domain
    else "lattice";

  hostName = config.networking.hostName;
  statusHost = "status.${hostName}.${domain}";
  statusDocument = builtins.toJSON {
    service = "lattice-node-status";
    node = hostName;
  };
in
{
  imports = [ ../tcp-gateway/config.nix ];

  config.services.caddy.virtualHosts."http://${statusHost}".extraConfig = ''
    header Content-Type application/json
    respond `${statusDocument}` 200
  '';
}
