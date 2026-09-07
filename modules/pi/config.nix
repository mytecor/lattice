{ config, lib, pkgs, ... }:

{
  config = lib.mkIf config.lattice.pi.enable {
    environment.systemPackages = [ pkgs.lattice.pi ];
  };
}
