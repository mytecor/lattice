{ nixpkgs, pkgs, gatewayProfile, llmGatewayModule }:

# Contract test for the external (mesh) ingress added in f4-05.
#
# profiles/tcp-gateway gained a `lattice.tcp-gateway.meshDomain` option: when
# it is set, every enabled service is also served on `http(s)://<service>.<meshDomain>`
# in parallel to the LAN contract `http://<service>.<node>.local`. This is what
# makes the node reachable from outside (from the Yggdrasil mesh) without a
# public IP or port forwarding.
#
# What must hold (semantic, not snapshot):
#   - with meshDomain set, an enabled service has BOTH a LAN and a mesh site,
#     and the mesh site routes to the same backend as the LAN site;
#   - the mesh site scheme is http when no Cloudflare token is provided and
#     https when one is (DNS-01 via acme_dns cloudflare);
#   - without meshDomain (default), no mesh sites are generated — the profile
#     stays LAN-only;
#   - services in meshExclude keep ONLY their LAN site even when meshDomain is
#     set (used for internal-only services like grafana/llm-gateway that must
#     not be reachable from the outside).

let
  inherit (nixpkgs) lib;

  hostName = "node-a";
  meshDomain = "homelab.myt.su";

  buildConfig = { useMesh ? true, useCloudflare ? false, meshExclude ? [ ] }:
    (lib.nixosSystem {
      modules = [
        gatewayProfile
        llmGatewayModule
        {
          nixpkgs.pkgs = nixpkgs.legacyPackages.x86_64-linux;
          networking.hostName = hostName;
          system.stateVersion = "26.05";
          lattice.tcp-gateway.meshDomain = if useMesh then meshDomain else null;
          lattice.tcp-gateway.cloudflareToken =
            if useCloudflare then "/run/agenix/caddy-cloudflare-token" else null;
          lattice.tcp-gateway.meshExclude = meshExclude;
          # Enable the LLM gateway route (it is one of the proxiedServices).
          lattice.llm-gateway.enable = true;
          lattice.llm-gateway.host = "127.0.0.1";
          lattice.llm-gateway.port = 9208;
          # The test only validates the generated Caddy config, never runs it:
          # silence the per-vhost access-log write (same fix as grafana-ingress).
          services.caddy.virtualHosts."http://llm-gateway.${hostName}.local".logFormat =
            lib.mkForce "output discard";
        }
      ];
    }).config;
in
let
  lanHost = "http://llm-gateway.${hostName}.local";
  meshHostNoTls = "http://llm-gateway.${meshDomain}";
  meshHostTls = "https://llm-gateway.${meshDomain}";

  cfg = buildConfig { };
  cfgMesh = buildConfig { useMesh = true; useCloudflare = false; };
  cfgTls = buildConfig { useMesh = true; useCloudflare = true; };
  cfgOff = buildConfig { useMesh = false; };
  cfgExcluded = buildConfig { useMesh = true; meshExclude = [ "llm-gateway" ]; };
in
assert !(builtins.hasAttr meshHostNoTls cfgOff.services.caddy.virtualHosts);

# mesh on, no TLS: both hosts exist, same backend
assert builtins.hasAttr lanHost cfgMesh.services.caddy.virtualHosts;
assert builtins.hasAttr meshHostNoTls cfgMesh.services.caddy.virtualHosts;
assert cfgMesh.services.caddy.virtualHosts.${lanHost}.extraConfig
  == cfgMesh.services.caddy.virtualHosts.${meshHostNoTls}.extraConfig;
# plain HTTP, no https port, default (unmodified) caddy package
assert !builtins.elem 443 cfgMesh.networking.firewall.allowedTCPPorts;
assert cfgMesh.services.caddy.package == cfg.services.caddy.package;

# mesh on, with Cloudflare token: https mesh host + 443 + plugin/environmentFile still
# compile (scheme flip is checked via the hostname set here).
assert builtins.hasAttr meshHostTls cfgTls.services.caddy.virtualHosts;
assert builtins.elem 443 cfgTls.networking.firewall.allowedTCPPorts;
assert cfgTls.services.caddy.environmentFile == "/run/agenix/caddy-cloudflare-token";
assert lib.hasInfix "acme_dns cloudflare" (cfgTls.services.caddy.globalConfig or "");

# meshExclude: the LAN site survives, the mesh site is NOT generated.
assert builtins.hasAttr lanHost cfgExcluded.services.caddy.virtualHosts;
assert !(builtins.hasAttr meshHostNoTls cfgExcluded.services.caddy.virtualHosts);
assert !(builtins.hasAttr meshHostTls cfgExcluded.services.caddy.virtualHosts);

pkgs.runCommand "tcp-gateway-mesh-evaluation" { } "touch $out"
