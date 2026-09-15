{ nixpkgs, pkgs, observabilityModules, observabilityProfile, gatewayProfile }:

# Contract test for the Grafana LAN ingress through the TCP gateway (F12).
#
# Grafana is loopback-only by design (see modules/grafana). The only way an
# operator reaches it is the operator-controlled Caddy ingress published by
# profiles/tcp-gateway: `http://grafana.<node>.local/` with an Avahi mDNS
# address alias registered by a per-service publisher.
#
# Verifies the *semantically important* properties of that wiring, not exact
# values:
#   - Grafana still binds 127.0.0.1 (non-public) even when the ingress exists;
#   - the Caddy site for `grafana.<node>.local` reverse-proxies to that
#     loopback listener (host:port), so clients never see storage hosts;
#   - the per-service mDNS publisher unit exists and publishes the grafana
#     alias;
#   - Caddy remains the only external listener on :80; the Grafana backend port
#     is not opened in the firewall (loopback is reachable from Caddy without
#     a firewall rule).

let
  inherit (nixpkgs) lib;
  hostName = "node-a";
  config = (lib.nixosSystem {
    modules = observabilityModules ++ [
      observabilityProfile
      gatewayProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = hostName;
        system.stateVersion = "26.05";
        lattice.grafana.adminPasswordFile = "/run/agenix/grafana-admin-password";
        lattice.grafana.secretKeyFile = "/run/agenix/grafana-secret-key";

        # The test only validates the generated Caddy config (caddy adapt
        # --validate) and never actually runs Caddy. The default per-vhost
        # access-log writes to /var/log/caddy, which does not exist inside the
        # Nix build sandbox and fails validation with `mkdir /var: permission
        # denied` (same fix as pi-acp-daemon.nix / git-cache-proxy-config.nix).
        services.caddy.virtualHosts."http://grafana.${hostName}.local".logFormat =
          lib.mkForce "output discard";
      }
    ];
  }).config;

  site = config.services.caddy.virtualHosts."http://grafana.${hostName}.local";
  grafana = config.lattice.grafana;
in
assert config.services.grafana.enable;
assert grafana.listenAddress == "127.0.0.1";
assert lib.hasInfix "reverse_proxy" site.extraConfig;
assert lib.hasInfix
  "${grafana.listenAddress}:${toString grafana.port}" site.extraConfig;
# The Grafana backend port stays out of the firewall: Caddy reaches it over
# loopback without any rule.
assert !(lib.elem grafana.port config.networking.firewall.allowedTCPPorts);
# Per-service mDNS publisher for the grafana alias exists and mentions the
# alias hostname.
assert builtins.hasAttr "grafana-mdns" config.systemd.services;
assert lib.hasInfix "grafana.${hostName}.local" config.systemd.services.grafana-mdns.script;
# Caddy is the only external HTTP listener.
assert config.services.caddy.enable;
assert !config.services.nginx.enable;
assert builtins.elem 80 config.networking.firewall.allowedTCPPorts;
pkgs.runCommand "grafana-ingress-evaluation" {
  nativeBuildInputs = [ pkgs.caddy ];
} ''
  mkdir -p "$out"

  export XDG_DATA_HOME="$TMPDIR/caddy-data"
  export XDG_CONFIG_HOME="$TMPDIR/caddy-config"
  caddy adapt \
    --config ${config.services.caddy.configFile} \
    --adapter caddyfile \
    --validate > "$out/caddy-config.json"

  echo "grafana LAN ingress contract holds" > "$out/result"
''
