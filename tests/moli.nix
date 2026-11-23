{ nixpkgs, pkgs, self, profiles }:
let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      self.nixosModules.authentik
      self.nixosModules.foxbridge-camoufox
      "${profiles}/sso/config.nix"
      "${profiles}/app-services/config.nix"
      "${profiles}/moli/config.nix"
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "viewer-test";
        lattice.foxbridge-camoufox = { enable = true; port = 19444; };
        lattice.moli.apiKeysFile = "/run/test-cdp-api-keys";
        system.stateVersion = "26.05";
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
  unit = config.systemd.services.browser-ingress;
  lan = config.services.caddy.virtualHosts."http://browser.viewer-test.local".extraConfig;
  mesh = config.services.caddy.virtualHosts."http://browser.mesh.example.org".extraConfig;
  ingressPort = (import ../profiles/networking/ports.nix).browser-ingress;
  caddyfile = pkgs.writeText "moli.Caddyfile" ''
    http://browser.viewer-test.local {
      ${lan}
    }
    http://browser.mesh.example.org {
      ${mesh}
    }
  '';
  smokeCaddyfile = pkgs.writeText "moli-smoke.Caddyfile" ''
    {
      admin off
    }
    http://browser.test:19225 {
      bind 127.0.0.1
      ${lib.replaceStrings
        [ "127.0.0.1:${toString ingressPort}" "127.0.0.1:${toString config.lattice.authentik.port}" ]
        [ "127.0.0.1:19224" "127.0.0.1:19226" ]
        lan}
    }
  '';
in
# The runtime credential is handed to the ingress rather than embedded in Caddy.
assert unit.serviceConfig.DynamicUser;
assert lib.elem "cdp-api-keys:${config.lattice.moli.apiKeysFile}" unit.serviceConfig.LoadCredential;
assert lib.hasInfix "--api-keys-file %d/cdp-api-keys" unit.serviceConfig.ExecStart;
assert lib.hasInfix "--foxbridge http://127.0.0.1:${toString config.lattice.foxbridge-camoufox.port}" unit.serviceConfig.ExecStart;
assert lib.hasInfix "--host 127.0.0.1" config.systemd.services.moli.serviceConfig.ExecStart;
assert lib.hasInfix "browser.viewer-test.local" config.systemd.services.browser-mdns.script;
assert lib.all (port: !(lib.elem port config.networking.firewall.allowedTCPPorts)) [
  (import ../profiles/networking/ports.nix).moli-cdp
  (import ../profiles/networking/ports.nix).browser-ingress
  config.lattice.foxbridge-camoufox.port
];
assert lib.all (site:
  lib.hasInfix "forward_auth" site
  && lib.hasInfix "/outpost.goauthentik.io/auth/caddy" site
  && lib.hasInfix "reverse_proxy 127.0.0.1:" site
  && lib.hasInfix "uri /_cdp_auth" site
) [ lan mesh ];
pkgs.runCommand "browser-ingress" {
  nativeBuildInputs = [ pkgs.caddy pkgs.lattice.moli pkgs.lattice.browser-ingress
    (pkgs.python3.withPackages (ps: [ ps.websockets ])) ];
} ''
  caddy adapt --config ${caddyfile} --adapter caddyfile --validate > "$out"
  export HOME="$TMPDIR"
  moli serve --host 127.0.0.1 --port 19223 --layout --resource > moli.log 2>&1 &
  browser_pid=$!
  printf '%s\n' smoke-key-a smoke-key-b > cdp-api-keys
  browser-ingress --listen 127.0.0.1:19224 --upstream http://127.0.0.1:19223 --foxbridge http://127.0.0.1:19223 --frontend ${pkgs.lattice.devtools-frontend} --api-keys-file cdp-api-keys > ingress.log 2>&1 &
  ingress_pid=$!
  caddy run --config ${smokeCaddyfile} --adapter caddyfile > caddy.log 2>&1 &
  caddy_pid=$!
  trap 'kill "$browser_pid" "$ingress_pid" "$caddy_pid" 2>/dev/null || true' EXIT
  python ${./moli-smoke.py} || { cat moli.log ingress.log caddy.log; exit 1; }
''
