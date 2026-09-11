{ lib, ... }:

let
  inherit (lib) types mkOption;
in
{
  options.lattice.hotspot = {
    enable = mkOption {
      type = types.bool;
      default = false;
      description = "Whether to run a concurrent STA+AP Wi-Fi hotspot on the same radio as the client (station) link.";
    };

    interfaceName = mkOption {
      type = types.str;
      default = "ap0";
      description = "Name of the virtual AP interface created on the shared radio.";
    };

    phy = mkOption {
      type = types.str;
      default = "phy0";
      description = "Name of the physical radio to add the AP virtual interface to (see `iw dev` / `iw phy`).";
    };

    ssid = mkOption {
      type = types.str;
      description = "SSID of the hotspot network.";
    };

    passwordFile = mkOption {
      type = types.path;
      description = "Path to a file containing the WPA2-PSK passphrase (8-63 ASCII characters, no trailing newline issues — the value is read as-is).";
    };

    channel = mkOption {
      type = types.int;
      default = 44;
      description = "RF channel for the AP. Because the RTL8822CE radio is `#channels <= 1`, this MUST equal the channel of the running STA link (home network), otherwise the radio would be switched off-channel and break the STA. Set it to the home network's channel.";
    };

    hwMode = mkOption {
      type = types.enum [ "a" "g" ];
      default = "a";
      description = "802.11 hardware mode: `a` for 5 GHz, `g` for 2.4 GHz. Must match the band of the STA link.";
    };

    countryCode = mkOption {
      type = types.str;
      default = "US";
      description = "IEEE 802.11d country code embedded in hostapd beacons.";
    };

    macAddress = mkOption {
      type = types.str;
      default = "02:0a:44:00:00:01";
      description = "Locally-administered MAC for the AP virtual interface. MUST differ from the STA interface MAC or rtw88 refuses to set the interface UP (`Name not unique on network`).";
    };

    ip = mkOption {
      type = types.str;
      default = "10.44.0.1/24";
      description = "Address and prefix assigned to the AP interface (node-side).";
    };

    routerIp = mkOption {
      type = types.str;
      default = "10.44.0.1";
      description = "Gateway address handed to DHCP clients (must equal the node-side IP without the prefix).";
    };

    subnet = mkOption {
      type = types.str;
      default = "10.44.0.0/24";
      description = "Hotspot subnet; used to build the MASQUERADE exception so local traffic is not NATed.";
    };

    dhcpRange = mkOption {
      type = types.str;
      default = "10.44.0.10,10.44.0.100,255.255.255.0,12h";
      description = "dnsmasq `dhcp-range` value (start,end,netmask,lease).";
    };

    dnsServers = mkOption {
      type = types.listOf types.str;
      default = [ "1.1.1.1" "8.8.8.8" ];
      description = "Upstream DNS servers advertised to hotspot clients.";
    };

    staInterface = mkOption {
      type = types.str;
      default = "wlp2s0";
      description = "Name of the station (client) interface. Kept for documentation and future checks; NetworkManager manages it as usual.";
    };
  };
}
