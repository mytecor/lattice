{ nixpkgs, pkgs, hotspotModule }:

# Validation of the concurrent STA+AP wireless hotspot module
# (modules/wireless-hotspot). Evaluates isolated configurations (auto band/channel
# and explicitly pinned 5 GHz / channel 44) and asserts the emitted systemd units,
# the generated hostapd config (incl. the mandatory `vht_oper_chwidth=0`, unique
# MAC and shared-channel constraints), the DHCP/NAT wiring, the NetworkManager
# unmanaged marking and the ip_forward sysctl.
let
  lib = nixpkgs.lib;
  mk = extra: (lib.nixosSystem {
    modules = [
      hotspotModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.hotspot = {
          enable = true;
          ssid = "Mytecor Homelab";
          passwordFile = "/tmp/fake-psk";
          staInterface = "wlp2s0";
        } // extra;
      }
    ];
  }).config;

  # Auto band/channel (default): the AP must track the STA's channel.
  auto = mk { };
  autoScript = auto.systemd.services.lattice-hotspot-conf.script;

  # Explicitly pinned to 5 GHz / channel 44 (as the original task intent).
  pinned = mk { channel = 44; hwMode = "a"; };
  pinnedScript = pinned.systemd.services.lattice-hotspot-conf.script;

  # The generated hostapd config script (materialises the passphrase secret).
  confScript = auto.systemd.services.lattice-hotspot-conf.script;
in
assert auto.lattice.hotspot.enable;
# NetworkManager must leave the AP interface alone.
assert auto.networking.networkmanager.unmanaged == [ "ap0" ];
# hostapd unit supervises the foreground daemon from the generated config.
assert lib.hasInfix "hostapd /run/lattice-hotspot/hostapd.conf"
  auto.systemd.services.lattice-hotspot.serviceConfig.ExecStart;
assert auto.systemd.services.lattice-hotspot.serviceConfig.Type == "simple";
# Mandatory rtw88 constraints in the generated conf: the SSID, WPA2-PSK/CCMP,
# and 20 MHz (`vht_oper_chwidth=0`) on the 5 GHz path.
assert lib.hasInfix "ssid=Mytecor Homelab" confScript;
assert lib.hasInfix "wpa_passphrase=$psk" confScript;
assert lib.hasInfix "rsn_pairwise=CCMP" confScript;
assert lib.hasInfix "wpa=2" confScript;
assert lib.hasInfix "vht_oper_chwidth=0" confScript;
assert lib.hasInfix "ieee80211ac" confScript;
# Auto-detection: band+channel derived from the live STA link, not hard-coded.
assert lib.hasInfix "iw dev wlp2s0 info" autoScript;
assert lib.hasInfix "hw_mode=$hw_mode" autoScript;
assert lib.hasInfix "channel=$channel" autoScript;
# Pinned mode forces the requested channel/band and 20 MHz (assigns vars that
# the common prologue then renders as channel=/hw_mode=).
assert lib.hasInfix "channel=\"44\"" pinnedScript;
assert lib.hasInfix "hw_mode=\"a\"" pinnedScript;
assert lib.hasInfix "-n \"44\"" pinnedScript;
assert lib.hasInfix "vht_oper_chwidth=0" pinnedScript;
# Interface is created idempotently with a unique MAC before hostapd starts.
assert lib.hasInfix "iw phy phy0 interface add ap0 type __ap"
  auto.systemd.services.lattice-hotspot.preStart;
assert lib.hasInfix "ip link set ap0 address 02:0a:44:00:00:01"
  auto.systemd.services.lattice-hotspot.preStart;
assert lib.hasInfix "ip addr add 10.44.0.1/24 dev ap0"
  auto.systemd.services.lattice-hotspot.preStart;
# DHCP wired to the AP interface with the expected pool.
assert auto.services.dnsmasq.enable;
assert lib.elem "ap0" auto.services.dnsmasq.settings.interface;
assert lib.elem "10.44.0.10,10.44.0.100,255.255.255.0,12h"
  auto.services.dnsmasq.settings.dhcp-range;
assert lib.elem "option:router,10.44.0.1" auto.services.dnsmasq.settings.dhcp-option;
# Forwarding accepts lane and NAT masquerades the hotspot subnet.
assert lib.hasInfix "FORWARD -i ap0 -j ACCEPT"
  auto.networking.firewall.extraForwardRules;
assert lib.hasInfix "POSTROUTING -s 10.44.0.0/24 ! -d 10.44.0.0/24 -j MASQUERADE"
  auto.systemd.services.lattice-hotspot-nat.script;
# IP forwarding enabled for the NAT to work.
assert auto.boot.kernel.sysctl."net.ipv4.ip_forward" == "1";
pkgs.runCommand "wireless-hotspot-module-check" { } "touch $out"
