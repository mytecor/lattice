{ lib, ... }:

let
  inherit (lib) types mkOption;
in
{
  options.lattice.hotspot-switch = {
    enable = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to enable dynamic Wi-Fi hotspot switching. When a wired
        Ethernet uplink with an internet default route is detected, Wi-Fi STA
        is disconnected and the radio switches to an Access Point (AP) with
        DHCP and NAT. When the cable is disconnected, the node cleanly reverts
        to Wi-Fi client mode.
      '';
    };

    ethInterfaces = mkOption {
      type = types.nullOr (types.listOf types.str);
      default = null;
      description = ''
        List of Ethernet interfaces to monitor as candidate uplink default-routes.
        When null (default), all physical Ethernet interfaces on the system are
        automatically detected.
      '';
    };

    wifiInterface = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Name of the station (client) Wi-Fi interface managed by NetworkManager.
        When null (default), the Wi-Fi interface is automatically detected.
      '';
    };

    phy = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = ''
        Name of the physical wireless device (e.g. phy0) on which to create the AP virtual interface.
        When null (default), it is automatically derived from the Wi-Fi interface.
      '';
    };

    ap = {
      interfaceName = mkOption {
        type = types.str;
        default = "ap0";
        description = "Name of the virtual AP interface created when entering AP mode.";
      };

      ssid = mkOption {
        type = types.str;
        description = "SSID of the hotspot network.";
      };

      passwordFile = mkOption {
        type = types.nullOr types.path;
        default = null;
        description = "Path to a file containing the WPA2-PSK passphrase.";
      };

      channel = mkOption {
        type = types.int;
        default = 36;
        description = "RF channel for the AP. Defaults to channel 36 (5 GHz).";
      };

      hwMode = mkOption {
        type = types.enum [ "a" "g" ];
        default = "a";
        description = "802.11 hardware mode: `a` for 5 GHz, `g` for 2.4 GHz.";
      };

      countryCode = mkOption {
        type = types.str;
        default = "US";
        description = "IEEE 802.11d country code embedded in hostapd beacons.";
      };

      macAddress = mkOption {
        type = types.str;
        default = "02:0a:44:00:00:01";
        description = "Locally-administered MAC for the AP interface. Must differ from the physical hardware MAC.";
      };

      ip = mkOption {
        type = types.str;
        default = "10.44.0.1/24";
        description = "IP address and CIDR prefix assigned to the AP interface.";
      };

      routerIp = mkOption {
        type = types.str;
        default = "10.44.0.1";
        description = "Default gateway advertised to DHCP clients on the hotspot network.";
      };

      subnet = mkOption {
        type = types.str;
        default = "10.44.0.0/24";
        description = "Hotspot IPv4 subnet; used for firewall FORWARD and NAT MASQUERADE rules.";
      };

      dhcpRange = mkOption {
        type = types.str;
        default = "10.44.0.10,10.44.0.100,255.255.255.0,12h";
        description = "dnsmasq dhcp-range parameter (start,end,netmask,lease).";
      };

      dnsServers = mkOption {
        type = types.listOf types.str;
        default = [ "1.1.1.1" "8.8.8.8" ];
        description = "Upstream DNS servers advertised to hotspot clients.";
      };

      vht = mkOption {
        type = types.bool;
        default = false;
        description = "Enable 802.11ac (VHT) on 5 GHz.";
      };
    };
  };
}
