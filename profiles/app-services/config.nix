{ config, lib, pkgs, ... }:

let
  hostName = config.networking.hostName;
  statusHost = "status.${hostName}.local";
  statusDocument = builtins.toJSON {
    service = "lattice-node-status";
    node = hostName;
  };
in
{
  imports = [ ../tcp-gateway/config.nix ];

  config = {
    services.caddy.virtualHosts."http://${statusHost}".extraConfig = ''
      header Content-Type application/json
      respond `${statusDocument}` 200
    '';

    systemd.services.node-status-mdns = {
      description = "Publish the node status mDNS alias";
      wantedBy = [ "multi-user.target" ];
      after = [ "avahi-daemon.service" "network-online.target" ];
      requires = [ "avahi-daemon.service" ];
      wants = [ "network-online.target" ];
      script = ''
        address="$(${pkgs.iproute2}/bin/ip -4 -o route get 1.1.1.1 \
          | ${pkgs.gawk}/bin/awk '{ for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }')"
        if [ -z "$address" ]; then
          echo "could not determine the primary IPv4 address" >&2
          exit 1
        fi
        exec ${config.services.avahi.package}/bin/avahi-publish \
          --address --no-reverse ${lib.escapeShellArg statusHost} "$address"
      '';
      serviceConfig = {
        Restart = "always";
        RestartSec = 5;
      };
    };
  };
}
