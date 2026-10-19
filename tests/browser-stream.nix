{ nixpkgs, pkgs, self, profiles }:
let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      self.nixosModules.authentik
      self.nixosModules.foxbridge-camoufox
      "${profiles}/sso/config.nix"
      "${profiles}/app-services/config.nix"
      "${profiles}/browser-stream/config.nix"
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "viewer-test";
        system.stateVersion = "26.05";
        lattice.foxbridge-camoufox = { enable = true; port = 19444; };
        lattice.authentik = {
          enable = true;
          secretKeyFile = "/run/test-secret";
          bootstrapTokenFile = "/run/test-token";
          bootstrapUserFile = "/run/test-user";
          bootstrapEmailFile = "/run/test-email";
          bootstrapPasswordFile = "/run/test-password";
          forwardAuth = [ { service = "browser"; } ];
        };
        lattice.tcp-gateway.meshDomain = "mesh.example.org";
      }
    ];
  }).config;
  unit = config.systemd.services.browser-stream;
  lan = config.services.caddy.virtualHosts."http://browser.viewer-test.local".extraConfig;
  mesh = config.services.caddy.virtualHosts."http://browser.mesh.example.org".extraConfig;
  caddyfile = pkgs.writeText "browser-stream.Caddyfile" ''
    http://browser.viewer-test.local {
      ${lan}
    }
    http://browser.mesh.example.org {
      ${mesh}
    }
  '';
in
# Generated artifacts: custom CDP port flows to the runtime, loopback listener
# stays private, SSO covers the entire proxy (including WS), mDNS is published.
assert unit.environment.BROWSER_CDP_URL == "http://127.0.0.1:19444";
assert !(lib.elem (lib.toInt unit.environment.BROWSER_STREAM_PORT) config.networking.firewall.allowedTCPPorts);
assert unit.serviceConfig.DynamicUser;
assert lib.hasInfix "browser.viewer-test.local" config.systemd.services.browser-mdns.script;
assert lib.all (site:
  lib.hasInfix "forward_auth" site
  && lib.hasInfix "/outpost.goauthentik.io/auth/caddy" site
  && lib.hasInfix "reverse_proxy 127.0.0.1:${unit.environment.BROWSER_STREAM_PORT}" site
  && !(lib.hasInfix "@" site)
) [ lan mesh ];
pkgs.runCommand "browser-stream-ingress" { nativeBuildInputs = [ pkgs.caddy ]; } ''
  caddy adapt --config ${caddyfile} --adapter caddyfile --validate > "$out"
''
