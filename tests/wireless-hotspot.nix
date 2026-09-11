{ nixpkgs, pkgs, hotspotModule }:

# Validation of the concurrent STA+AP wireless hotspot module
# (modules/wireless-hotspot). Evaluates isolated configurations (auto band/channel
# and explicitly pinned 5 GHz / channel 44) and asserts the emitted hostapd unit,
# its preStart (interface creation with a unique MAC + hostapd config generation
# incl. the mandatory `vht_oper_chwidth=0` and shared-channel constraints), the
# DHCP/NAT wiring, the NetworkManager unmanaged marking and the ip_forward sysctl.
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
  autoPre = auto.systemd.services.lattice-hotspot.preStart;

  # Explicitly pinned to 5 GHz / channel 44 (as the original task intent).
  pinned = mk { channel = 44; hwMode = "a"; };
  pinnedPre = pinned.systemd.services.lattice-hotspot.preStart;
in
assert auto.lattice.hotspot.enable;
# NetworkManager must leave the AP interface alone.
assert auto.networking.networkmanager.unmanaged == [ "ap0" ];
# hostapd unit supervises the foreground daemon from the generated config.
assert lib.hasInfix "hostapd /run/lattice-hotspot/hostapd.conf"
  auto.systemd.services.lattice-hotspot.serviceConfig.ExecStart;
assert auto.systemd.services.lattice-hotspot.serviceConfig.Type == "simple";
assert auto.systemd.services.lattice-hotspot.serviceConfig.Restart == "on-failure";
# Mandatory rtw88 constraints in the generated conf: unique MAC, WPA2-PSK/CCMP,
# the SSID, and 20 MHz (`vht_oper_chwidth=0`) on the 5 GHz path.
assert lib.hasInfix "ip link set ap0 address 02:0a:44:00:00:01" autoPre;
# Idempotent ap0 (re)creation: only recreate when missing or with a stale MAC,
# and restart dnsmasq afterwards so its DHCP socket stays bound to ap0.
assert lib.hasInfix "ip link show ap0" autoPre;
assert lib.hasInfix "iw phy phy0 interface add ap0 type __ap" autoPre;
assert lib.hasInfix "systemctl restart dnsmasq" autoPre;
# dnsmasq starts only after the ap0 interface exists.
assert lib.elem "lattice-hotspot.service"
  auto.systemd.services.dnsmasq.after;
assert lib.hasInfix "ip addr add 10.44.0.1/24 dev ap0" autoPre;
assert lib.hasInfix "ssid=Mytecor Homelab" autoPre;
assert lib.hasInfix "wpa_passphrase=$psk" autoPre;
assert lib.hasInfix "rsn_pairwise=CCMP" autoPre;
assert lib.hasInfix "wpa=2" autoPre;
assert lib.hasInfix "vht_oper_chwidth=0" autoPre;
assert lib.hasInfix "vht_oper_chwidth=0" pinnedPre;
# Auto-detection: band+channel derived from the live STA link, not hard-coded.
assert lib.hasInfix "iw dev wlp2s0 info" autoPre;
assert lib.hasInfix "hw_mode=$hw_mode" autoPre;
assert lib.hasInfix "channel=$channel" autoPre;
# The STA freq parser reads the `(freq MHz` token (field 3), not a later field.
assert lib.hasInfix "print $3" autoPre;
# On 5 GHz the AP matches the STA's channel width (80 MHz) instead of forcing
# 20 MHz: parse width/center1, and drive vht_oper_chwidth + seg0 from them.
assert lib.hasInfix "width:" autoPre;
assert lib.hasInfix "center1:" autoPre;
assert lib.hasInfix "vht_oper_chwidth=$vht_oper_chwidth" autoPre;
assert lib.hasInfix "vht_oper_centr_freq_seg0_idx=$vht_seg0" autoPre;
# Graceful fallback when the STA carrier is not up yet: channel=auto (0), so a
# boot-time preStart does not fail the unit.
assert lib.hasInfix "channel=0" autoPre;
# Pinned mode forces the requested channel/band and 20 MHz (assigns vars that
# the common prologue then renders as channel=/hw_mode=).
assert lib.hasInfix "channel=\"44\"" pinnedPre;
assert lib.hasInfix "hw_mode=\"a\"" pinnedPre;
assert lib.hasInfix "-n \"44\"" pinnedPre;
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
