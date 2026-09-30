{ nixpkgs, pkgs, hotspotSwitchModule }:

# Module contract test and evaluation smoke test for lattice.hotspot-switch (F17).
#
# Verifies:
# 1. Compile-time assertions and option contracts (isolated evaluation).
# 2. Systemd units, firewall rules, NetworkManager integration, and kernel sysctl.
# 3. Rejection of invalid configurations (empty ethInterfaces, colliding names, etc.).
# 4. Runtime behavior of the evaluation / switching script (lattice-hotspot-switch):
#    - Automatic switch to AP when Ethernet uplink with internet default-route is active.
#    - Anti-flapping debounce protection.
#    - Clean return to client mode when Ethernet carrier is lost.
#    - Idempotency in both states.
let
  lib = nixpkgs.lib;

  dummyPasswordFile = pkgs.writeText "test-hotspot-psk" "supersecretpassword123";

  mkConfig = extra: (lib.nixosSystem {
    modules = [
      hotspotSwitchModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        networking.hostName = "test-node";
        networking.networkmanager.enable = true;

        lattice.hotspot-switch = {
          enable = true;
          ethInterfaces = [ "enp3s0" ];
          wifiInterface = "wlp2s0";
          ap = {
            ssid = "Test Hotspot";
            passwordFile = dummyPasswordFile;
            channel = 36;
            hwMode = "a";
          };
        };
      }
      extra
    ];
  }).config;

  valid = mkConfig { };
  autoConfig = mkConfig {
    lattice.hotspot-switch.ethInterfaces = lib.mkForce null;
    lattice.hotspot-switch.wifiInterface = lib.mkForce null;
  };
  nftConfig = mkConfig {
    networking.firewall.backend = "nftables";
  };

  # Assertion failure helper
  failures = config: map (item: item.message) (lib.filter
    (item: !item.assertion && lib.hasPrefix "lattice.hotspot-switch" item.message) config.assertions);

  badEmptyEth = mkConfig { lattice.hotspot-switch.ethInterfaces = lib.mkForce [ ]; };
  badEmptyWifi = mkConfig { lattice.hotspot-switch.wifiInterface = lib.mkForce ""; };
  badSameIface = mkConfig { lattice.hotspot-switch.wifiInterface = lib.mkForce "ap0"; };
  badEthCollision = mkConfig { lattice.hotspot-switch.ethInterfaces = lib.mkForce [ "ap0" ]; };
  badEmptySsid = mkConfig { lattice.hotspot-switch.ap.ssid = lib.mkForce ""; };
  badNullPassword = mkConfig { lattice.hotspot-switch.ap.passwordFile = lib.mkForce null; };
  badFirewallBackend = mkConfig { networking.firewall.backend = "firewalld"; };

  # Extract script from valid config
  switchPkg = lib.findFirst (p: p.name == "lattice-hotspot-switch") null valid.environment.systemPackages;
  switchBin = "${switchPkg}/bin/lattice-hotspot-switch";
in
# 1. Contract & Invariants
assert valid.lattice.hotspot-switch.enable;
assert autoConfig.lattice.hotspot-switch.enable;
assert autoConfig.lattice.hotspot-switch.ethInterfaces == null;
assert autoConfig.lattice.hotspot-switch.wifiInterface == null;
assert lib.elem "ap0" valid.networking.networkmanager.unmanaged;
assert valid.boot.kernel.sysctl."net.ipv4.ip_forward" == "1";
assert lib.hasInfix "iptables -w -A FORWARD -i ap0 -j ACCEPT" valid.networking.firewall.extraCommands;
assert lib.hasInfix "iptables -w -A FORWARD -o ap0" valid.networking.firewall.extraCommands;
assert lib.hasInfix ''iifname "ap0" accept'' nftConfig.networking.firewall.extraForwardRules;
assert lib.hasInfix ''oifname "ap0" ct state { established, related } accept'' nftConfig.networking.firewall.extraForwardRules;

# Systemd units
assert valid.systemd.services.lattice-hotspot-ap.serviceConfig.Type == "simple";
assert valid.systemd.services.lattice-hotspot-ap.serviceConfig.Restart == "on-failure";
assert lib.elem "lattice-hotspot-dnsmasq.service" valid.systemd.services.lattice-hotspot-ap.wants;
assert lib.elem "lattice-hotspot-ap.service" valid.systemd.services.lattice-hotspot-dnsmasq.bindsTo;
assert valid.systemd.services.lattice-hotspot-switch.serviceConfig.Type == "oneshot";
assert valid.systemd.paths.lattice-hotspot-switch-carrier-enp3s0.pathConfig.PathModified == "/sys/class/net/enp3s0/carrier";

# Dispatcher script
assert builtins.length valid.networking.networkmanager.dispatcherScripts == 1;

# 2. Module assertions on invalid inputs
assert failures valid == [ ];
assert failures autoConfig == [ ];
assert lib.elem "lattice.hotspot-switch: ethInterfaces cannot be empty if specified." (failures badEmptyEth);
assert lib.elem "lattice.hotspot-switch: wifiInterface cannot be empty string if specified." (failures badEmptyWifi);
assert lib.elem "lattice.hotspot-switch: ap.interfaceName must differ from wifiInterface (ap0)." (failures badSameIface);
assert lib.elem "lattice.hotspot-switch: ap.interfaceName cannot be in ethInterfaces." (failures badEthCollision);
assert lib.elem "lattice.hotspot-switch: ap.ssid must not be empty." (failures badEmptySsid);
assert lib.elem "lattice.hotspot-switch: ap.passwordFile must be specified." (failures badNullPassword);
assert lib.elem "lattice.hotspot-switch supports only the iptables and nftables firewall backends." (failures badFirewallBackend);

# 3. Smoke test for lattice-hotspot-switch evaluator logic under simulated conditions
pkgs.runCommand "hotspot-switch-eval-test" {
  nativeBuildInputs = [ pkgs.coreutils pkgs.bash pkgs.gawk ];
} ''
  set -eu

  TEST_DIR="$PWD/test-env"
  mkdir -p "$TEST_DIR/sysfs/enp3s0" "$TEST_DIR/sysfs/wlp2s0" "$TEST_DIR/run" "$TEST_DIR/bin"

  SYSFS="$TEST_DIR/sysfs"
  RUN_DIR="$TEST_DIR/run"
  BIN_DIR="$TEST_DIR/bin"

  # Mock `ip` command
  cat << 'EOF' > "$BIN_DIR/mock-ip"
  #!/usr/bin/env bash
  if [ "$1" = "-4" ] && [ "$2" = "route" ] && [ "$3" = "get" ]; then
    cat "$TEST_DIR/mock-route" 2>/dev/null || { echo "RTNETLINK answers: Network is unreachable" >&2; exit 1; }
  else
    exit 0
  fi
  EOF
  chmod +x "$BIN_DIR/mock-ip"

  # Mock `systemctl` command
  cat << 'EOF' > "$BIN_DIR/mock-systemctl"
  #!/usr/bin/env bash
  ACTION="$1"
  UNIT="''${2:-}"
  echo "$ACTION $UNIT" >> "$TEST_DIR/systemctl-calls.log"

  if [ "$ACTION" = "is-active" ]; then
    if [ -f "$TEST_DIR/ap-is-active" ]; then
      exit 0
    else
      exit 3
    fi
  elif [ "$ACTION" = "start" ]; then
    touch "$TEST_DIR/ap-is-active"
    echo "ap" > "$RUN_DIR/mode"
    exit 0
  elif [ "$ACTION" = "stop" ]; then
    rm -f "$TEST_DIR/ap-is-active"
    echo "client" > "$RUN_DIR/mode"
    exit 0
  fi
  EOF
  chmod +x "$BIN_DIR/mock-systemctl"

  export IP_CMD="$BIN_DIR/mock-ip"
  export SYS_CLASS_NET="$SYSFS"
  export SYSTEMCTL_CMD="$BIN_DIR/mock-systemctl"
  export TEST_DIR
  export RUN_DIR="$RUN_DIR"
  export MODE_FILE="$RUN_DIR/mode"
  export LOCK_FILE="$RUN_DIR/switch.lock"
  export DEBOUNCE_SEC="0"
  export ETH_INTERFACES_OVERRIDE="enp3s0"

  # Helper to run the switch tool
  run_switch() {
    ${valid.lattice.hotspot-switch.environment.systemPackages or ""}/bin/lattice-hotspot-switch "$@"
  }

  SWITCH_CMD="${switchBin}"

  # --- Test Case 1: Initial state is client mode ---
  initial_mode=$("$SWITCH_CMD" mode)
  [ "$initial_mode" = "client" ]

  # --- Test Case 2: Ethernet uplink active with default route -> switch to AP ---
  echo "1" > "$SYSFS/enp3s0/carrier"
  echo "1.1.1.1 via 192.168.3.1 dev enp3s0 src 192.168.3.12 uid 1000" > "$TEST_DIR/mock-route"

  "$SWITCH_CMD" eval

  mode_after_eth=$("$SWITCH_CMD" mode)
  [ "$mode_after_eth" = "ap" ]
  grep -q "start lattice-hotspot-ap.service" "$TEST_DIR/systemctl-calls.log"

  # --- Test Case 3: Idempotent when already in AP mode ---
  > "$TEST_DIR/systemctl-calls.log"
  "$SWITCH_CMD" eval
  # No new start call should be made
  if grep -q "start lattice-hotspot-ap.service" "$TEST_DIR/systemctl-calls.log"; then
    echo "ERROR: switch was not idempotent in AP mode" >&2
    exit 1
  fi
  [ "$("$SWITCH_CMD" mode)" = "ap" ]

  # --- Test Case 4: Ethernet carrier lost -> return to client mode ---
  > "$TEST_DIR/systemctl-calls.log"
  echo "0" > "$SYSFS/enp3s0/carrier"
  # Default route falls back to wifi
  echo "1.1.1.1 via 192.168.60.1 dev wlp2s0 src 192.168.60.184 uid 1000" > "$TEST_DIR/mock-route"

  "$SWITCH_CMD" eval

  mode_after_disconnect=$("$SWITCH_CMD" mode)
  [ "$mode_after_disconnect" = "client" ]
  grep -q "stop lattice-hotspot-ap.service" "$TEST_DIR/systemctl-calls.log"

  # --- Test Case 5: Wi-Fi default route only -> does NOT switch to AP ---
  > "$TEST_DIR/systemctl-calls.log"
  "$SWITCH_CMD" eval
  [ "$("$SWITCH_CMD" mode)" = "client" ]
  if grep -q "start lattice-hotspot-ap.service" "$TEST_DIR/systemctl-calls.log"; then
    echo "ERROR: erroneously switched to AP when default route is Wi-Fi" >&2
    exit 1
  fi

  # --- Test Case 6: Ethernet cable inserted but no default route -> stays in client ---
  echo "1" > "$SYSFS/enp3s0/carrier"
  echo "1.1.1.1 via 192.168.60.1 dev wlp2s0 src 192.168.60.184 uid 1000" > "$TEST_DIR/mock-route"
  "$SWITCH_CMD" eval
  [ "$("$SWITCH_CMD" mode)" = "client" ]

  # --- Test Case 7: Anti-flapping debounce aborts if uplink drops during wait ---
  export DEBOUNCE_SEC="1"
  echo "1.1.1.1 via 192.168.3.1 dev enp3s0 src 192.168.3.12 uid 1000" > "$TEST_DIR/mock-route"
  # In background, drop carrier after 0.2s during the 1s debounce window
  ( sleep 0.2 && echo "0" > "$SYSFS/enp3s0/carrier" ) &
  "$SWITCH_CMD" eval
  wait
  [ "$("$SWITCH_CMD" mode)" = "client" ]

  # --- Test Case 8: is-uplink-active CLI check ---
  echo "0" > "$SYSFS/enp3s0/carrier"
  if "$SWITCH_CMD" is-uplink-active; then
    echo "ERROR: is-uplink-active returned 0 when carrier is 0" >&2
    exit 1
  fi
  echo "1" > "$SYSFS/enp3s0/carrier"
  if ! "$SWITCH_CMD" is-uplink-active; then
    echo "ERROR: is-uplink-active returned non-zero when uplink is active" >&2
    exit 1
  fi

  # --- Test Case 9: Interface auto-detection (get-wifi-interface, get-eth-interfaces) ---
  mkdir -p "$SYSFS/wlp2s0/wireless"
  mkdir -p "$SYSFS/enp3s0/device"
  echo "1" > "$SYSFS/enp3s0/type"

  wifi_detected=$("$SWITCH_CMD" get-wifi-interface)
  [ "$wifi_detected" = "wlp2s0" ]

  unset ETH_INTERFACES_OVERRIDE
  eth_detected=$("$SWITCH_CMD" get-eth-interfaces)
  echo "$eth_detected" | grep -q "enp3s0"

  mkdir -p "$out"
  echo "All hotspot switch tests passed successfully." > "$out/success"
''
