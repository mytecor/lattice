{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.hotspot;
  apWanted = lib.mkIf cfg.enable;
  # Explicit channel override: when null the band/channel is auto-detected from
  # the live STA link at config-generation time.
  hotspotChannelOverride =
    lib.optionalString (cfg.channel != null) "${toString cfg.channel}";
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
  # Resolve the AP's channel/band. RTL8822CE is `#channels <= 1`, so the AP must
  # share the STA's channel. Prefer an explicit `channel`/`hwMode` when set;
  # otherwise auto-detect the band and channel from the live STA link via iw at
  # config-generation time so the hotspot tracks whatever the home network uses
  # (2.4 or 5 GHz) and survives a home-network band change.
  systemd.services.lattice-hotspot-conf = apWanted {
    description = "Generate hostapd configuration for ${cfg.interfaceName} hotspot";
    wantedBy = [ "multi-user.target" ];
    before = [ "lattice-hotspot.service" ];
    path = [ pkgs.coreutils pkgs.gawk pkgs.iw ];
    script = ''
      set -eu
      [ -r ${lib.escapeShellArg cfg.passwordFile} ] || {
        echo "lattice-hotspot: passphrase file not readable: ${lib.escapeShellArg cfg.passwordFile}" >&2
        exit 1
      }
      psk=$(cat ${lib.escapeShellArg cfg.passwordFile})

      if [ -n "${hotspotChannelOverride}" ]; then
        channel="${hotspotChannelOverride}"
        hw_mode="${if cfg.hwMode != null then cfg.hwMode else "a"}"
      else
        # channel N (freq MHz), width: ... from `iw dev STA info`
        freq=$(iw dev ${cfg.staInterface} info 2>/dev/null | awk '/^[[:space:]]*channel/ {print $4}' | tr -d '()')
        if [ -z "$freq" ]; then
            echo "lattice-hotspot: could not read STA channel from ${cfg.staInterface}; is it connected?" >&2
            exit 1
        fi
        if [ "$freq" -ge 5000 ]; then
          hw_mode="a"
          channel=$(( (freq - 5000) / 5 ))
          vht=1
        else
          hw_mode="g"
          channel=$(( (freq - 2407) / 5 ))
          vht=0
        fi
      fi

      mkdir -p /run/lattice-hotspot
      umask 077
      {
        echo "interface=${cfg.interfaceName}"
        echo "driver=nl80211"
        echo "ssid=${cfg.ssid}"
        echo "hw_mode=$hw_mode"
        echo "channel=$channel"
        echo "ieee80211n=1"
        if [ "${if cfg.hwMode != null then "1" else "$vht"}" = "1" ]; then
          echo "ieee80211ac=1"
          echo "vht_oper_chwidth=0"
        fi
        echo "wmm_enabled=1"
        echo "wpa=2"
        echo "wpa_key_mgmt=WPA-PSK"
        echo "rsn_pairwise=CCMP"
        echo "wpa_passphrase=$psk"
        echo "auth_algs=1"
        echo "ignore_broadcast_ssid=0"
        echo "country_code=${cfg.countryCode}"
      } > /run/lattice-hotspot/hostapd.conf
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
