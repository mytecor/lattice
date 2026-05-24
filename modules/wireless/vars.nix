{ lib, networks }:

let
  indexedNetworks = lib.imap1 (index: net: {
    inherit net;
    name = "wifi-${toString index}";
    ssidVar = "LATTICE_WIFI_SSID_${toString index}";
    passwordVar = "LATTICE_WIFI_PSK_${toString index}";
  }) networks;
in
{
  envFile = "/run/lattice-wireless.env";

  inherit indexedNetworks;

  secretFiles = lib.concatMap (network: [
    network.net.ssid
    network.net.password
  ]) indexedNetworks;
}
