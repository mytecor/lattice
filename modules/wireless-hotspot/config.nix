{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.hotspot;
  apWanted = lib.mkIf cfg.enable;
in
{
  # The AP shares the same radio as the STA link, so NetworkManager must not try
  # to own the virtual AP interface (it would flip it to managed mode, swap its
  # MAC and fight hostapd for the radio).
  networking.networkmanager.unmanaged = apWanted [ cfg.interfaceName ];

  # Generate the hostapd config at runtime from the WPA passphrase secret file,
  # mirroring the STA wireless secret handling (modules/wireless). The passphrase
  # lives in an age secret, not in the NixOS store, so it must be materialised to
  # a fresh config at boot time.
  systemd.services.lattice-hotspot-conf = apWanted {
    description = "Generate hostapd configuration for ${cfg.interfaceName} hotspot";
    wantedBy = [ "multi-user.target" ];
    before = [ "lattice-hotspot.service" ];
    path = [ pkgs.coreutils ];
    script = ''
      set -eu
      [ -r ${lib.escapeShellArg cfg.passwordFile} ] || {
        echo "lattice-hotspot: passphrase file not readable: ${lib.escapeShellArg cfg.passwordFile}" >&2
        exit 1
      }
      psk=$(cat ${lib.escapeShellArg cfg.passwordFile})
      mkdir -p /run/lattice-hotspot
      umask 077
      cat > /run/lattice-hotspot/hostapd.conf <<EOF
      interface=${cfg.interfaceName}
      driver=nl80211
      ssid=${cfg.ssid}
      hw_mode=${cfg.hwMode}
      channel=${toString cfg.channel}
      ieee80211n=1
      ieee80211ac=1
      vht_oper_chwidth=0
      wmm_enabled=1
      wpa=2
      wpa_key_mgmt=WPA-PSK
      rsn_pairwise=CCMP
      wpa_passphrase=$psk
      auth_algs=1
      ignore_broadcast_ssid=0
      country_code=${cfg.countryCode}
      EOF
    '';
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
      UMask = "0177";
    };
  };

  # hostapd does not create the interface itself. A tiny wrapper unit creates the
  # virtual AP interface on the shared radio, assigns a unique MAC and address,
  # then starts hostapd in the foreground so systemd supervises it.
  systemd.services.lattice-hotspot = apWanted {
    description = "hostapd Wi-Fi hotspot on ${cfg.interfaceName} (concurrent STA+AP)";
    after = [ "network.target" "lattice-hotspot-conf.service" ];
    wants = [ "network.target" ];
    wantedBy = [ "multi-user.target" ];
    path = [ pkgs.iw pkgs.iproute2 pkgs.hostapd ];
    preStart = ''
      # Idempotent recreation of the AP vif on every (re)start.
      iw dev ${cfg.interfaceName} del 2>/dev/null || true
      iw phy ${cfg.phy} interface add ${cfg.interfaceName} type __ap
      # A unique locally-administered MAC is required: with the STA's own MAC the
      # RTL8822CE driver refuses UP (`Name not unique on network`).
      ip link set ${cfg.interfaceName} address ${cfg.macAddress}
      ip link set ${cfg.interfaceName} up
      ip addr flush dev ${cfg.interfaceName}
      ip addr add ${cfg.ip} dev ${cfg.interfaceName}
    '';
    serviceConfig = {
      Type = "simple";
      ExecStart = "${pkgs.hostapd}/bin/hostapd /run/lattice-hotspot/hostapd.conf";
      Restart = "on-failure";
      RestartSec = "3s";
    };
  };

  # DHCP + DNS for the hotspot subnet.
  services.dnsmasq = apWanted {
    enable = true;
    settings = {
      interface = cfg.interfaceName;
      bind-interfaces = true;
      dhcp-range =  [ cfg.dhcpRange ];
      dhcp-option = [
        "option:router,${cfg.routerIp}"
        "option:dns-server,${lib.concatStringsSep "," cfg.dnsServers}"
      ];
      no-resolv = true;
      server = cfg.dnsServers;
    };
  };

  # Internet for hotspot clients: forward AP->internet and reply traffic, and
  # MASQUERADE the hotspot subnet out of the node. FORWARD rules live in the
  # firewall's filter table; the NAT rule is applied by a small idempotent unit
  # because NixOS's firewall module has no nat-table hook.
  networking.firewall.extraForwardRules = apWanted ''
    iptables -A FORWARD -i ${cfg.interfaceName} -j ACCEPT
    iptables -A FORWARD -o ${cfg.interfaceName} -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
  '';

  systemd.services.lattice-hotspot-nat = apWanted {
    description = "MASQUERADE NAT for ${cfg.interfaceName} hotspot clients";
    after = [ "lattice-hotspot.service" ];
    wantedBy = [ "multi-user.target" ];
    path = [ pkgs.iptables ];
    script = ''
      set -eu
      # Idempotent: only append if not already present.
      if ! iptables -t nat -C POSTROUTING -s ${cfg.subnet} ! -d ${cfg.subnet} -j MASQUERADE 2>/dev/null; then
        iptables -t nat -A POSTROUTING -s ${cfg.subnet} ! -d ${cfg.subnet} -j MASQUERADE
      fi
    '';
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = true;
    };
  };

  boot.kernel.sysctl."net.ipv4.ip_forward" = apWanted "1";
}
