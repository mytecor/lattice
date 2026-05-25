{ pkgs, example-package, ... }:

{
  networking.hostName = "example";

  environment.systemPackages = [
    example-package.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];

  system.stateVersion = "26.05";
}
