{ pkgs, example-package, ... }:

{
  networking.hostName = "example";

  lattice.wipe-root.subvolume = "@root";

  environment.systemPackages = [
    example-package.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];

  system.stateVersion = "26.05";
}
