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

  # hostapd does not create the interface itself. A small wrapper unit creates
  # the virtual AP interface on the shared radio (unique MAC, address), then in
  # preStart regenerates the hostapd config from the passphrase age secret and
  # the live STA channel, and finally runs hostapd in the foreground so systemd
  # supervises it. Putting config generation in preStart means the channel is
  # re-resolved on every start/restart, so a `Restart=on-failure` or a config
  # switch always picks up the current STA band/channel.
  systemd.services.lattice-hotspot = apWanted {
    description = "hostapd Wi-Fi hotspot on ${cfg.interfaceName} (concurrent STA+AP)";
    after = [ "network.target" ];
    wants = [ "network.target" ];
    wantedBy = [ "multi-user.target" ];
    path = [ pkgs.iw pkgs.iproute2 pkgs.coreutils pkgs.gawk pkgs.hostapd ];
    preStart = ''
      # Idempotent: only (re)create the AP vif when it is missing or carries a
      # different MAC. Recreating it unconditionally on every restart would drop
      # the interface out from under dnsmasq, whose DHCP socket is bound to ap0,
      # leaving hotspot clients without an IP (no leases until a manual dnsmasq
      # restart). dnsmasq is restarted once whenever ap0 is (re)created.
      if ! ip link show ${cfg.interfaceName} >/dev/null 2>&1 || \
         [ "$(cat /sys/class/net/${cfg.interfaceName}/address 2>/dev/null)" != "${cfg.macAddress}" ]; then
        iw dev ${cfg.interfaceName} del 2>/dev/null || true
        iw phy ${cfg.phy} interface add ${cfg.interfaceName} type __ap
        # A unique locally-administered MAC is required: with the STA's own MAC
        # the RTL8822CE driver refuses UP (`Name not unique on network`).
        ip link set ${cfg.interfaceName} address ${cfg.macAddress}
        ip link set ${cfg.interfaceName} up
        ip addr flush dev ${cfg.interfaceName}
        ip addr add ${cfg.ip} dev ${cfg.interfaceName}
        # Rebind the DHCP socket to the fresh ap0.
        systemctl restart dnsmasq
      fi

      # Regenerate hostapd.conf. RTL8822CE is `#channels <= 1`, so the AP must
      # share the STA's channel: prefer an explicit channel/hwMode override, else
      # auto-detect the band+channel from the live STA link.
      [ -r ${lib.escapeShellArg cfg.passwordFile} ] || {
        echo "lattice-hotspot: passphrase file not readable: ${lib.escapeShellArg cfg.passwordFile}" >&2
        exit 1
      }
      psk=$(cat ${lib.escapeShellArg cfg.passwordFile})
      # 5 GHz VHT fields; on 2.4 GHz / pinned mode these stay 20 MHz (chwidth=0, no seg0).
      vht_oper_chwidth=0
      vht_seg0=

      if [ -n "${hotspotChannelOverride}" ]; then
        channel="${hotspotChannelOverride}"
        hw_mode="${if cfg.hwMode != null then cfg.hwMode else "a"}"
        vht=1
      else
        # `channel N (freq MHz), width: W MHz, center1: C MHz` from `iw dev STA info`.
        # Token 3 `(freq` -> strip the paren for the freq; take the STA's channel
        # width and center1 so the AP matches the STA's 80 MHz block (a forced 20 MHz
        # AP inside an 80 MHz STA on the same RTL8822CE radio breaks the data path).
        sta_info=$(iw dev ${cfg.staInterface} info 2>/dev/null)
        freq=$(printf '%s\n' "$sta_info" | awk '/^[[:space:]]*channel/ {print $3}' | tr -d '()')
        w=$(printf '%s\n' "$sta_info" | awk '/^[[:space:]]*channel/{for(i=1;i<=NF;i++) if($i=="width:"){gsub("MHz","",$(i+1)); print $(i+1)}}')
        c1=$(printf '%s\n' "$sta_info" | awk '/^[[:space:]]*channel/{for(i=1;i<=NF;i++) if($i=="center1:"){print $(i+1)}}')
        if [ "$freq" -ge 5000 ]; then
          hw_mode="a"
          channel=$(( (freq - 5000) / 5 ))
          vht=1
          if [ "$w" -ge 80 ]; then
            vht_oper_chwidth=1
            vht_seg0=$(( (c1 - 5000) / 5 ))
          elif [ "$w" -ge 40 ]; then
            vht_oper_chwidth=0
            vht_seg0=$(( (c1 - 5000) / 5 ))
          fi
        elif [ "$freq" -ge 2400 ]; then
          hw_mode="g"
          channel=$(( (freq - 2407) / 5 ))
          vht=0
        else
          # STA carrier not established yet (e.g. during boot before Wi-Fi is up).
          # Do not fail the unit: hand over channel=auto (0); Restart=on-failure
          # re-runs preStart once the STA link has a channel.
          echo "lattice-hotspot: STA channel unknown, using channel=auto (0)" >&2
          hw_mode="a"
          channel=0
          vht=1
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
        if [ "$vht" = "1" ]; then
          echo "ieee80211ac=1"
          echo "vht_oper_chwidth=$vht_oper_chwidth"
          if [ -n "$vht_seg0" ]; then
            echo "vht_oper_centr_freq_seg0_idx=$vht_seg0"
          fi
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
      Type = "simple";
      ExecStart = "${pkgs.hostapd}/bin/hostapd /run/lattice-hotspot/hostapd.conf";
      Restart = "on-failure";
      RestartSec = "3s";
    };
  };

  # DHCP + DNS for the hotspot subnet.
  # dnsmasq binds its DHCP socket to ap0, which is (re)created by the
  # lattice-hotspot unit's preStart; make sure it starts only after ap0 exists
  # (and is restarted there whenever ap0 is recreated).
  systemd.services.dnsmasq = apWanted {
    after = [ "lattice-hotspot.service" ];
  };

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
