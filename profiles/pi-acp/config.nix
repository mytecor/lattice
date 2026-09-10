{ config, lib, ... }:

let
  latticePorts = import ../networking/ports.nix;
in
{
  lattice.pi-acp-daemon = {
    enable = true;
    port = lib.mkDefault latticePorts.pi-acp;
    user = lib.mkDefault config.lattice.pi.user;
    group = lib.mkDefault (
      config.users.users.${config.lattice.pi.user}.group or config.lattice.pi.user
    );
  };
}
