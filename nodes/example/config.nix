{ pkgs, ... }:

let
  # TEST DATA ONLY: writeText exposes these values in the world-readable Nix store.
  # Real nodes must use config.age.secrets.<name>.path instead.
  testWirelessFile = name: value: "${pkgs.writeText "lattice-example-${name}" value}";
in

{
  networking.hostName = "example";

  lattice.wireless.networks = [
    {
      ssid = testWirelessFile "wifi-home-ssid" "Example Home";
      password = testWirelessFile "wifi-home-password" "example-home-password";
    }
    {
      ssid = testWirelessFile "wifi-office-ssid" "Example Office";
      password = testWirelessFile "wifi-office-password" "example-office-password";
    }
  ];

  system.stateVersion = "26.05";
}
