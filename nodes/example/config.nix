{ pkgs, example-package, ... }:

let
  fakeWirelessSecret = name: value: "${pkgs.writeText "lattice-example-${name}" value}";
in

{
  networking.hostName = "example";

  lattice.wireless.networks = [
    {
      ssid = fakeWirelessSecret "wifi-home-ssid" "Example Home";
      password = fakeWirelessSecret "wifi-home-password" "example-home-password";
    }
    {
      ssid = fakeWirelessSecret "wifi-office-ssid" "Example Office";
      password = fakeWirelessSecret "wifi-office-password" "example-office-password";
    }
  ];

  environment.systemPackages = [
    example-package.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];

  system.stateVersion = "26.05";
}
