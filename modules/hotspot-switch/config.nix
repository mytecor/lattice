{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.hotspot-switch;

  ethConfiguredStr = if cfg.ethInterfaces != null then lib.concatStringsSep " " cfg.ethInterfaces else "";
  wifiConfiguredStr = if cfg.wifiInterface != null then cfg.wifiInterface else "";
  phyConfiguredStr = if cfg.phy != null then cfg.phy else "";

  # CLI utility and switcher script
  switchScript = pkgs.writeShellScriptBin "lattice-hotspot-switch" ''
    set -euo pipefail

    IP_CMD="''${IP_CMD:-${pkgs.iproute2}/bin/ip}"
    SYS_CLASS_NET="''${SYS_CLASS_NET:-/sys/class/net}"
    SYSTEMCTL_CMD="''${SYSTEMCTL_CMD:-${pkgs.systemd}/bin/systemctl}"
    RUN_DIR="''${RUN_DIR:-/run/lattice-hotspot-switch}"
    MODE_FILE="''${MODE_FILE:-$RUN_DIR/mode}"
    LOCK_FILE="''${LOCK_FILE:-$RUN_DIR/switch.lock}"
    DEBOUNCE_SEC="''${DEBOUNCE_SEC:-2}"

    ETH_CONFIGURED="${ethConfiguredStr}"
    WIFI_CONFIGURED="${wifiConfiguredStr}"
    PHY_CONFIGURED="${phyConfiguredStr}"
    AP_IFACE="${cfg.ap.interfaceName}"

    mkdir -p "$RUN_DIR"

    # Resolves Wi-Fi interface (configured or auto-detected)
    get_wifi_interface() {
      if [ -n "''${WIFI_INTERFACE_OVERRIDE:-}" ]; then
        echo "''${WIFI_INTERFACE_OVERRIDE}"
        return 0
      fi
      if [ -n "$WIFI_CONFIGURED" ]; then
        echo "$WIFI_CONFIGURED"
        return 0
      fi
      for ifpath in "$SYS_CLASS_NET"/*; do
        [ -d "$ifpath" ] || continue
        local ifname
        ifname=$(basename "$ifpath")
        [ "$ifname" = "$AP_IFACE" ] && continue
        if [ -d "$ifpath/wireless" ] || [ -d "$ifpath/phy80211" ]; then
          echo "$ifname"
          return 0
        fi
      done
      echo "wlp2s0"
    }

    # Resolves physical wireless device (configured or derived from wifi interface)
    get_phy() {
      if [ -n "''${PHY_OVERRIDE:-}" ]; then
        echo "''${PHY_OVERRIDE}"
        return 0
      fi
      if [ -n "$PHY_CONFIGURED" ]; then
        echo "$PHY_CONFIGURED"
        return 0
      fi
      local wifi_if
      wifi_if=$(get_wifi_interface)
      if [ -r "$SYS_CLASS_NET/$wifi_if/phy80211/name" ]; then
        cat "$SYS_CLASS_NET/$wifi_if/phy80211/name"
        return 0
      fi
      echo "phy0"
    }

    # Resolves candidate Ethernet interfaces (configured or auto-detected physical eth)
    get_eth_interfaces() {
      if [ -n "''${ETH_INTERFACES_OVERRIDE:-}" ]; then
        echo "''${ETH_INTERFACES_OVERRIDE}"
        return 0
      fi
      if [ -n "$ETH_CONFIGURED" ]; then
        echo "$ETH_CONFIGURED"
        return 0
      fi
      local eths=()
      for ifpath in "$SYS_CLASS_NET"/*; do
        [ -d "$ifpath" ] || continue
        local ifname
        ifname=$(basename "$ifpath")
        [ "$ifname" = "lo" ] && continue
        [ "$ifname" = "$AP_IFACE" ] && continue
        # Must not be wireless
        [ -d "$ifpath/wireless" ] && continue
        [ -d "$ifpath/phy80211" ] && continue
        # Must not be tunnel/vpn/bridge/container/virtual
        if echo "$ifname" | ${pkgs.gnugrep}/bin/grep -qE '^(ygg|tun|tap|wg|br|veth|docker|virbr)'; then
          continue
        fi
        # Must be Ethernet type 1 (ARPHRD_ETHER)
        local iftype
        iftype=$(cat "$ifpath/type" 2>/dev/null || echo 0)
        [ "$iftype" = "1" ] || continue
        # Must have device symlink or follow predictable ethernet naming
        if [ -d "$ifpath/device" ] || echo "$ifname" | ${pkgs.gnugrep}/bin/grep -qE '^(en|eth)'; then
          eths+=("$ifname")
        fi
      done
      echo "''${eths[*]}"
    }

    has_eth_uplink() {
      local route_out
      route_out=$("$IP_CMD" -4 route get 1.1.1.1 2>/dev/null || true)
      [ -z "$route_out" ] && return 1

      local out_dev
      out_dev=$(echo "$route_out" | ${pkgs.gawk}/bin/awk '{for(i=1;i<=NF;i++) if($i=="dev") print $(i+1)}' | head -n1)
      [ -z "$out_dev" ] && return 1

      local candidate_eths
      candidate_eths=$(get_eth_interfaces)

      local is_candidate=0
      for eth in $candidate_eths; do
        if [ "$eth" = "$out_dev" ]; then
          is_candidate=1
          break
        fi
      done
      [ "$is_candidate" -eq 1 ] || return 1

      local carrier_file="$SYS_CLASS_NET/$out_dev/carrier"
      if [ -r "$carrier_file" ]; then
        local carrier
        carrier=$(cat "$carrier_file" 2>/dev/null || echo 0)
        [ "$carrier" = "1" ] || return 1
      fi

      return 0
    }

    cmd_mode() {
      if [ -r "$MODE_FILE" ]; then
        cat "$MODE_FILE"
      else
        echo "client"
      fi
    }

    cmd_is_uplink_active() {
      if has_eth_uplink; then
        return 0
      else
        return 1
      fi
    }

    cmd_status() {
      local wifi_if
      wifi_if=$(get_wifi_interface)
      local phy
      phy=$(get_phy)
      local eths
      eths=$(get_eth_interfaces)

      echo "Hotspot Switch Status:"
      echo "  Current Mode: $(cmd_mode)"
      echo "  Wi-Fi Interface: $wifi_if (phy: $phy)"
      echo "  Candidate Ethernet Interfaces: $eths"
      for eth in $eths; do
        local carrier="missing"
        if [ -r "$SYS_CLASS_NET/$eth/carrier" ]; then
          carrier=$(cat "$SYS_CLASS_NET/$eth/carrier" 2>/dev/null || echo "unknown")
        fi
        echo "    $eth carrier: $carrier"
      done
      local route_out
      route_out=$("$IP_CMD" -4 route get 1.1.1.1 2>/dev/null || echo "unreachable")
      echo "  Route for 1.1.1.1: $route_out"
      echo "  AP Service active: $("$SYSTEMCTL_CMD" is-active lattice-hotspot-ap.service 2>/dev/null || echo "inactive")"
    }

    cmd_eval() {
      exec 200>"$LOCK_FILE"
      if ! ${pkgs.util-linux}/bin/flock -n 200; then
        echo "Hotspot evaluation lock busy, another instance is running." >&2
        exit 0
      fi

      local current_mode
      current_mode=$(cmd_mode)

      local target_mode="client"
      if has_eth_uplink; then
        # Anti-flapping check: when transitioning from client to AP, wait and reconfirm
        if [ "$current_mode" != "ap" ] && [ "$DEBOUNCE_SEC" -gt 0 ]; then
          sleep "$DEBOUNCE_SEC"
          if ! has_eth_uplink; then
            echo "Ethernet uplink flapped during confirmation window; staying in client mode" >&2
            exit 0
          fi
        fi
        target_mode="ap"
      else
        target_mode="client"
      fi

      local is_ap_active=0
      if "$SYSTEMCTL_CMD" is-active --quiet lattice-hotspot-ap.service 2>/dev/null; then
        is_ap_active=1
      fi

      if [ "$target_mode" = "ap" ]; then
        if [ "$is_ap_active" -eq 1 ] && [ "$current_mode" = "ap" ]; then
          return 0
        fi
        echo "Switching to AP mode (Ethernet uplink active)"
        if ! "$SYSTEMCTL_CMD" start lattice-hotspot-ap.service; then
          echo "Failed to start lattice-hotspot-ap.service, falling back to client mode" >&2
          echo "client" > "$MODE_FILE"
          return 1
        fi
      else
        if [ "$is_ap_active" -eq 0 ] && [ "$current_mode" = "client" ]; then
          return 0
        fi
        echo "Switching to client mode (Ethernet uplink inactive)"
        "$SYSTEMCTL_CMD" stop lattice-hotspot-ap.service || true
        echo "client" > "$MODE_FILE"
      fi
    }

    ACTION="''${1:-eval}"
    case "$ACTION" in
      mode)
        cmd_mode
        ;;
      get-wifi-interface)
        get_wifi_interface
        ;;
      get-phy)
        get_phy
        ;;
      get-eth-interfaces)
        get_eth_interfaces
        ;;
      is-uplink-active)
        cmd_is_uplink_active
        ;;
      status)
        cmd_status
        ;;
      eval)
        cmd_eval
        ;;
      *)
        echo "Usage: lattice-hotspot-switch [mode|get-wifi-interface|get-phy|get-eth-interfaces|is-uplink-active|status|eval]" >&2
        exit 1
        ;;
    esac
  '';

  apStartPre = pkgs.writeShellScript "lattice-hotspot-ap-start-pre" ''
    set -euo pipefail

    # 1. Verify that Ethernet uplink is active
    if ! ${switchScript}/bin/lattice-hotspot-switch is-uplink-active; then
      echo "lattice-hotspot-ap: ethernet uplink is not active, aborting AP bringup" >&2
      exit 1
    fi

    WIFI_IF=$(${switchScript}/bin/lattice-hotspot-switch get-wifi-interface)
    PHY=$(${switchScript}/bin/lattice-hotspot-switch get-phy)

    # 2. Strict protection against simultaneous STA+AP:
    # Disconnect Wi-Fi STA in NetworkManager and mark device unmanaged
    ${pkgs.networkmanager}/bin/nmcli device disconnect "$WIFI_IF" 2>/dev/null || true
    ${pkgs.networkmanager}/bin/nmcli device set "$WIFI_IF" managed no 2>/dev/null || true

    # Wait for STA link to disconnect
    for i in {1..10}; do
      link_state=$(${pkgs.iw}/bin/iw dev "$WIFI_IF" link 2>/dev/null || echo "Not connected")
      if echo "$link_state" | ${pkgs.gnugrep}/bin/grep -q "Not connected"; then
        break
      fi
      sleep 0.5
    done

    # Force STA interface down so radio cannot send or receive client frames
    ${pkgs.iproute2}/bin/ip link set "$WIFI_IF" down 2>/dev/null || true

    link_state=$(${pkgs.iw}/bin/iw dev "$WIFI_IF" link 2>/dev/null || echo "Not connected")
    if ! echo "$link_state" | ${pkgs.gnugrep}/bin/grep -q "Not connected"; then
      echo "lattice-hotspot-ap: failed to disconnect STA interface $WIFI_IF, aborting to prevent concurrent STA+AP instability" >&2
      exit 1
    fi

    # 3. Create virtual AP interface
    ${pkgs.iw}/bin/iw dev "${cfg.ap.interfaceName}" del 2>/dev/null || true
    ${pkgs.iw}/bin/iw phy "$PHY" interface add "${cfg.ap.interfaceName}" type __ap
    ${pkgs.iproute2}/bin/ip link set "${cfg.ap.interfaceName}" address "${cfg.ap.macAddress}"
    ${pkgs.iproute2}/bin/ip link set "${cfg.ap.interfaceName}" up
    ${pkgs.iproute2}/bin/ip addr flush dev "${cfg.ap.interfaceName}"
    ${pkgs.iproute2}/bin/ip addr add "${cfg.ap.ip}" dev "${cfg.ap.interfaceName}"

    # 4. Generate configurations
    mkdir -p /run/lattice-hotspot-switch
    chmod 755 /run/lattice-hotspot-switch

    [ -r "${toString cfg.ap.passwordFile}" ] || {
      echo "lattice-hotspot-ap: password file not readable: ${toString cfg.ap.passwordFile}" >&2
      exit 1
    }
    psk=$(cat "${toString cfg.ap.passwordFile}")

    # hostapd.conf (mode 0600). Keep the static fragments in quoted heredocs so
    # module values cannot trigger shell expansion; append the runtime secret
    # separately so it is expanded only as data.
    umask 077
    cat << 'EOF' > /run/lattice-hotspot-switch/hostapd.conf
interface=${cfg.ap.interfaceName}
driver=nl80211
ssid=${cfg.ap.ssid}
hw_mode=${cfg.ap.hwMode}
channel=${toString cfg.ap.channel}
ieee80211n=1
${lib.optionalString (cfg.ap.vht && cfg.ap.hwMode == "a") ''
ieee80211ac=1
vht_oper_chwidth=0
''}
wmm_enabled=1
wpa=2
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
EOF
    printf 'wpa_passphrase=%s\n' "$psk" >> /run/lattice-hotspot-switch/hostapd.conf
    cat << 'EOF' >> /run/lattice-hotspot-switch/hostapd.conf
auth_algs=1
ignore_broadcast_ssid=0
country_code=${cfg.ap.countryCode}
EOF

    # dnsmasq.conf (mode 0644)
    umask 022
    cat << 'EOF' > /run/lattice-hotspot-switch/dnsmasq.conf
interface=${cfg.ap.interfaceName}
bind-interfaces
dhcp-range=${cfg.ap.dhcpRange}
dhcp-option=option:router,${cfg.ap.routerIp}
dhcp-option=option:dns-server,${lib.concatStringsSep "," cfg.ap.dnsServers}
no-resolv
${lib.concatMapStringsSep "\n" (s: "server=${s}") cfg.ap.dnsServers}
EOF

    # 5. NAT MASQUERADE
    if ! ${pkgs.iptables}/bin/iptables -t nat -C POSTROUTING -s ${cfg.ap.subnet} ! -d ${cfg.ap.subnet} -j MASQUERADE 2>/dev/null; then
      ${pkgs.iptables}/bin/iptables -t nat -A POSTROUTING -s ${cfg.ap.subnet} ! -d ${cfg.ap.subnet} -j MASQUERADE
    fi

    # 6. Record state
    echo "ap" > /run/lattice-hotspot-switch/mode
  '';

  apStartPost = pkgs.writeShellScript "lattice-hotspot-ap-start-post" ''
    # Re-enumerate interfaces for mDNS publishers so services publish on ap0
    ${pkgs.systemd}/bin/systemctl try-restart "*-mdns.service" 2>/dev/null || true
  '';

  apStopPost = pkgs.writeShellScript "lattice-hotspot-ap-stop-post" ''
    set +e

    WIFI_IF=$(${switchScript}/bin/lattice-hotspot-switch get-wifi-interface)

    # 1. Remove NAT rule
    ${pkgs.iptables}/bin/iptables -t nat -D POSTROUTING -s ${cfg.ap.subnet} ! -d ${cfg.ap.subnet} -j MASQUERADE 2>/dev/null

    # 2. Tear down and delete ap0
    ${pkgs.iproute2}/bin/ip link set "${cfg.ap.interfaceName}" down 2>/dev/null
    ${pkgs.iw}/bin/iw dev "${cfg.ap.interfaceName}" del 2>/dev/null

    # 3. Restore Wi-Fi STA in NetworkManager
    ${pkgs.networkmanager}/bin/nmcli device set "$WIFI_IF" managed yes 2>/dev/null

    # 4. Re-enumerate interfaces for mDNS publishers
    ${pkgs.systemd}/bin/systemctl try-restart "*-mdns.service" 2>/dev/null

    # 5. Record state
    mkdir -p /run/lattice-hotspot-switch
    echo "client" > /run/lattice-hotspot-switch/mode
  '';
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.ethInterfaces == null || cfg.ethInterfaces != [ ];
        message = "lattice.hotspot-switch: ethInterfaces cannot be empty if specified.";
      }
      {
        assertion = cfg.wifiInterface == null || cfg.wifiInterface != "";
        message = "lattice.hotspot-switch: wifiInterface cannot be empty string if specified.";
      }
      {
        assertion = cfg.wifiInterface == null || cfg.ap.interfaceName != cfg.wifiInterface;
        message = "lattice.hotspot-switch: ap.interfaceName must differ from wifiInterface (${toString cfg.wifiInterface}).";
      }
      {
        assertion = cfg.ethInterfaces == null || (!lib.elem cfg.ap.interfaceName cfg.ethInterfaces);
        message = "lattice.hotspot-switch: ap.interfaceName cannot be in ethInterfaces.";
      }
      {
        assertion = cfg.ap.ssid != "";
        message = "lattice.hotspot-switch: ap.ssid must not be empty.";
      }
      {
        assertion = cfg.ap.passwordFile != null;
        message = "lattice.hotspot-switch: ap.passwordFile must be specified.";
      }
      {
        assertion = config.networking.networkmanager.enable;
        message = "lattice.hotspot-switch requires networking.networkmanager.enable = true.";
      }
      {
        assertion = lib.elem config.networking.firewall.backend [ "iptables" "nftables" ];
        message = "lattice.hotspot-switch supports only the iptables and nftables firewall backends.";
      }
    ];

    # NetworkManager must never manage the virtual AP interface
    networking.networkmanager.unmanaged = [ cfg.ap.interfaceName ];

    # Dispatcher script to evaluate routes on interface changes
    networking.networkmanager.dispatcherScripts = [
      {
        source = pkgs.writeShellScript "lattice-hotspot-switch-trigger" ''
          ACTION="''${2:-}"
          case "$ACTION" in
            up|down|dhcp4-change|dhcp6-change|connectivity-change)
              ${pkgs.systemd}/bin/systemctl start --no-block lattice-hotspot-switch.service 2>/dev/null || true
              ;;
          esac
        '';
        type = "basic";
      }
    ];

    # Udev rule to trigger evaluation on link/carrier changes for any Ethernet interface
    services.udev.extraRules = ''
      SUBSYSTEM=="net", ACTION=="change", ATTR{type}=="1", TAG+="systemd", ENV{SYSTEMD_WANTS}+="lattice-hotspot-switch.service"
    '';

    # Forwarding rules for hotspot clients. extraForwardRules is nftables-only;
    # the default NixOS firewall backend still needs explicit iptables hooks.
    networking.firewall.extraForwardRules = lib.mkIf (config.networking.firewall.backend == "nftables") ''
      iifname "${cfg.ap.interfaceName}" accept
      oifname "${cfg.ap.interfaceName}" ct state { established, related } accept
    '';
    networking.firewall.extraCommands = lib.mkIf (config.networking.firewall.backend == "iptables") ''
      ${pkgs.iptables}/bin/iptables -w -C FORWARD -i ${cfg.ap.interfaceName} -j ACCEPT 2>/dev/null || \
        ${pkgs.iptables}/bin/iptables -w -A FORWARD -i ${cfg.ap.interfaceName} -j ACCEPT
      ${pkgs.iptables}/bin/iptables -w -C FORWARD -o ${cfg.ap.interfaceName} -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || \
        ${pkgs.iptables}/bin/iptables -w -A FORWARD -o ${cfg.ap.interfaceName} -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
    '';
    networking.firewall.extraStopCommands = lib.mkIf (config.networking.firewall.backend == "iptables") ''
      ${pkgs.iptables}/bin/iptables -w -D FORWARD -i ${cfg.ap.interfaceName} -j ACCEPT 2>/dev/null || true
      ${pkgs.iptables}/bin/iptables -w -D FORWARD -o ${cfg.ap.interfaceName} -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
    '';

    # Clients must reach the local DHCP/DNS listeners and Avahi on the AP
    # interface before any forwarding or service discovery can work. Keep
    # these ports scoped to ap0 rather than exposing dnsmasq on every uplink.
    networking.firewall.interfaces.${cfg.ap.interfaceName} = {
      allowedTCPPorts = [ 53 ];
      allowedUDPPorts = [ 53 67 5353 ];
    };

    boot.kernel.sysctl."net.ipv4.ip_forward" = "1";

    # CLI tool available in system packages
    environment.systemPackages = [ switchScript ];

    # Systemd carrier watch paths (if ethInterfaces explicitly configured)
    systemd.paths = lib.mkIf (cfg.ethInterfaces != null) (lib.listToAttrs (map (eth: {
      name = "lattice-hotspot-switch-carrier-${eth}";
      value = {
        description = "Watch carrier state on ${eth} for hotspot switch";
        wantedBy = [ "multi-user.target" ];
        pathConfig = {
          PathModified = "/sys/class/net/${eth}/carrier";
          Unit = "lattice-hotspot-switch.service";
        };
      };
    }) cfg.ethInterfaces));

    # Evaluation one-shot service
    systemd.services.lattice-hotspot-switch = {
      description = "Evaluate Ethernet uplink and switch hotspot mode";
      after = [ "network.target" "NetworkManager.service" ];
      wantedBy = [ "multi-user.target" ];
      path = [ pkgs.coreutils pkgs.gawk pkgs.gnugrep pkgs.iproute2 pkgs.util-linux pkgs.systemd ];
      serviceConfig = {
        Type = "oneshot";
        ExecStart = "${switchScript}/bin/lattice-hotspot-switch eval";
      };
    };

    # AP hostapd service
    systemd.services.lattice-hotspot-ap = {
      description = "hostapd Wi-Fi Hotspot on ${cfg.ap.interfaceName}";
      after = [ "network.target" "NetworkManager.service" ];
      wants = [ "lattice-hotspot-dnsmasq.service" ];
      startLimitIntervalSec = 30;
      startLimitBurst = 3;
      path = [
        pkgs.coreutils
        pkgs.gawk
        pkgs.gnugrep
        pkgs.iproute2
        pkgs.iptables
        pkgs.iw
        pkgs.networkmanager
        pkgs.systemd
        switchScript
      ];
      serviceConfig = {
        Type = "simple";
        ExecStart = "${pkgs.hostapd}/bin/hostapd /run/lattice-hotspot-switch/hostapd.conf";
        ExecStartPre = apStartPre;
        ExecStartPost = apStartPost;
        ExecStopPost = apStopPost;
        Restart = "on-failure";
        RestartSec = "3s";
      };
    };

    # DHCP/DNS dnsmasq service
    systemd.services.lattice-hotspot-dnsmasq = {
      description = "dnsmasq DHCP/DNS server for Wi-Fi hotspot";
      bindsTo = [ "lattice-hotspot-ap.service" ];
      after = [ "lattice-hotspot-ap.service" ];
      serviceConfig = {
        Type = "simple";
        ExecStart = "${pkgs.dnsmasq}/bin/dnsmasq -k --conf-file=/run/lattice-hotspot-switch/dnsmasq.conf";
        Restart = "on-failure";
        RestartSec = "2s";
      };
    };
  };
}
