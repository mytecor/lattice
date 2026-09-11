{ nixpkgs, pkgs, hotspotModule }:

# Validation of the concurrent STA+AP wireless hotspot module
# (modules/wireless-hotspot). Evaluates an isolated configuration with the
# hotspot enabled and asserts the emitted systemd units, the generated hostapd
# config (incl. the mandatory `vht_oper_chwidth=0`, unique-MAC and shared-channel
# constraints), the DHCP/NAT wiring, the NetworkManager unmanaged marking and the
# ip_forward sysctl.
let
  lib = nixpkgs.lib;
  config = (lib.nixosSystem {
    modules = [
      hotspotModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.hotspot = {
          enable = true;
          ssid = "Mytecor Homelab";
          passwordFile = "/tmp/fake-psk";
          channel = 44;
          hwMode = "a";
          staInterface = "wlp2s0";
        };
      }
    ];
  }).config;

  # The generated hostapd config script (materialises the passphrase secret).
  confScript = config.systemd.services.lattice-hotspot-conf.script;
in
assert config.lattice.hotspot.enable;
# NetworkManager must leave the AP interface alone.
assert config.networking.networkmanager.unmanaged == [ "ap0" ];
# hostapd unit supervises the foreground daemon from the generated config.
assert lib.hasInfix "hostapd /run/lattice-hotspot/hostapd.conf"
  config.systemd.services.lattice-hotspot.serviceConfig.ExecStart;
assert config.systemd.services.lattice-hotspot.serviceConfig.Type == "simple";
# Mandatory rtw88 constraints in the generated conf: 20 MHz (`vht_oper_chwidth=0`),
# a concrete shared channel, WPA2-PSK/CCMP, the 5 GHz hw_mode and the SSID.
assert lib.hasInfix "vht_oper_chwidth=0" confScript;
assert lib.hasInfix "channel=44" confScript;
assert lib.hasInfix "hw_mode=a" confScript;
assert lib.hasInfix "ssid=Mytecor Homelab" confScript;
assert lib.hasInfix "wpa_passphrase=$psk" confScript;
assert lib.hasInfix "rsn_pairwise=CCMP" confScript;
assert lib.hasInfix "wpa=2" confScript;
# Interface is created idempotently with a unique MAC before hostapd starts.
assert lib.hasInfix "iw phy phy0 interface add ap0 type __ap"
  config.systemd.services.lattice-hotspot.preStart;
assert lib.hasInfix "ip link set ap0 address 02:0a:44:00:00:01"
  config.systemd.services.lattice-hotspot.preStart;
assert lib.hasInfix "ip addr add 10.44.0.1/24 dev ap0"
  config.systemd.services.lattice-hotspot.preStart;
# DHCP wired to the AP interface with the expected pool.
assert config.services.dnsmasq.enable;
assert lib.elem "ap0" config.services.dnsmasq.settings.interface;
assert lib.elem "10.44.0.10,10.44.0.100,255.255.255.0,12h"
  config.services.dnsmasq.settings.dhcp-range;
assert lib.elem "option:router,10.44.0.1" config.services.dnsmasq.settings.dhcp-option;
# Forwarding accepts lane and NAT masquerades the hotspot subnet.
assert lib.hasInfix "FORWARD -i ap0 -j ACCEPT"
  config.networking.firewall.extraForwardRules;
assert lib.hasInfix "POSTROUTING -s 10.44.0.0/24 ! -d 10.44.0.0/24 -j MASQUERADE"
  config.systemd.services.lattice-hotspot-nat.script;
# IP forwarding enabled for the NAT to work.
assert config.boot.kernel.sysctl."net.ipv4.ip_forward" == "1";
pkgs.runCommand "wireless-hotspot-module-check" { } "touch $out"
