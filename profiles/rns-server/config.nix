{ ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  config.lattice.rns-server = {
    enable = true;

    interfaces."Auto Discovery" = {
      type = "AutoInterface";
      enabled = true;
      discovery_scope = "link";
      discovery_port = latticePorts.rns-auto-discovery;
      data_port = latticePorts.rns-auto-data;
    };
  };
}
