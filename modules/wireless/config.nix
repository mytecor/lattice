{ config, lib, ... }:

let
  cfg = config.lattice.wireless;
  dollar = "$";
  vars = import ./vars.nix { inherit lib; networks = cfg.networks; };
in
{
  networking.networkmanager = {
    enable = true;
    ensureProfiles = lib.mkIf (cfg.networks != [ ]) {
      environmentFiles = [ vars.envFile ];
      profiles = lib.listToAttrs (map (network: {
        name = network.name;
        value = {
          connection = {
            id = network.name;
            type = "wifi";
          };
          wifi = {
            mode = "infrastructure";
            ssid = "${dollar}${network.ssidVar}";
          };
          wifi-security = {
            auth-alg = "open";
            key-mgmt = "wpa-psk";
            psk = "${dollar}${network.passwordVar}";
          };
          ipv4.method = "auto";
          ipv6 = {
            addr-gen-mode = "stable-privacy";
            method = "auto";
          };
        };
      }) vars.indexedNetworks);
    };
  };

  networking.wireless.enable = lib.mkDefault false;
}
