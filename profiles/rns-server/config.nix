{ lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  config.lattice.rns-server = {
    enable = lib.mkDefault true;

    interfaces."Auto Discovery" = {
      type = lib.mkDefault "AutoInterface";
      enabled = lib.mkDefault true;
      discovery_scope = lib.mkDefault "link";
      discovery_port = lib.mkDefault latticePorts.rns-auto-discovery;
      data_port = lib.mkDefault latticePorts.rns-auto-data;
    };

    interfaces."TCP Server" = {
      type = lib.mkDefault "TCPServerInterface";
      enabled = lib.mkDefault true;
      listen_ip = lib.mkDefault "0.0.0.0";
      listen_port = lib.mkDefault latticePorts.rns-tcp;
      max_connections = lib.mkDefault 64;
    };

    # The entry point is selected in F3-02; enabling this requires target_host.
    interfaces."TCP Uplink" = {
      type = lib.mkDefault "TCPClientInterface";
      enabled = lib.mkDefault false;
      target_port = lib.mkDefault latticePorts.rns-tcp;
    };
  };
}
