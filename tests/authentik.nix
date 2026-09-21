{ nixpkgs, pkgs, authentikModule, ssoProfile, gatewayProfile, appServicesProfile, grafanaModule }:

# Contract test for the F14 Authentik SSO wiring (central identity behind the
# single Caddy ingress). Mirrors tests/grafana-ingress.nix.
#
# Verifies the *semantically important* properties, not exact values:
#   - Authentik still binds 127.0.0.1 (non-public) even with the ingress;
#   - the Caddy site for `auth.<node>.local` reverse-proxies to that loopback
#     listener (host:port), and the mesh site exists too (not meshExcluded —
#     the login page must be reachable from mesh clients);
#   - the per-service mDNS publisher for `auth` exists;
#   - the loopback backend port is NOT opened in the firewall;
#   - the systemd units run as the non-root `authentik` user (the `ak` wrapper
#     then skips its container-root branch);
#   - backend port stays out of the firewall: Caddy reaches Authentik over
#     loopback without a rule.

let
  inherit (nixpkgs) lib;
  hostName = "node-a";
  config = (lib.nixosSystem {
    modules = [
      authentikModule
      grafanaModule
      ssoProfile
      appServicesProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = hostName;
        system.stateVersion = "26.05";

        # Grafana enabled with native OIDC through Authentik (F14 step 7): the
        # client secret is an agenix runtime path read via the file provider.
        lattice.grafana = {
          enable = true;
          port = 3000;
          adminPasswordFile = "/run/agenix/grafana-admin-password";
          secretKeyFile = "/run/agenix/grafana-secret-key";
          oauth = {
            clientId = "grafana";
            clientSecretFile = "/run/agenix/grafana-oauth-client-secret";
            authUrl = "http://auth.${hostName}.local/application/o/authorize/";
            tokenUrl = "http://auth.${hostName}.local/application/o/token/";
            apiUrl = "http://auth.${hostName}.local/application/o/userinfo/";
            scopes = [ "openid" "profile" "email" ];
            adminGroup = "authentik Admins";
          };
        };

        # Seed the mandatory secrets with literal runtime paths (test-only).
        lattice.authentik.secretKeyFile = "/run/agenix/authentik-secret-key";
        lattice.authentik.bootstrapTokenFile = "/run/agenix/authentik-bootstrap-token";
        lattice.authentik.bootstrapUserFile = "/run/agenix/authentik-bootstrap-user";
        lattice.authentik.bootstrapEmailFile = "/run/agenix/authentik-bootstrap-email";
        lattice.authentik.bootstrapPasswordFile = "/run/agenix/authentik-bootstrap-password";
        # ForwardAuth the static acp-ui browser UI.
        lattice.authentik.forwardAuth = [
          { service = "acp-ui"; }
        ];

        # Enable the mesh ingress so the auth site's mesh host is generated
        # (login page is NOT meshExcluded — it must stay reachable from mesh
        # clients). The token is a literal runtime path (test-only).
        lattice.tcp-gateway.meshDomain = "homelab.myt.su";
        lattice.tcp-gateway.cloudflareToken = "/run/agenix/caddy-cloudflare-token";

        # The test only validates generated Caddy config (caddy adapt
        # --validate) and never operates Authentik. Keep the per-vhost access
        # log away from /var/log (same fix as grafana-ingress.nix).
        services.caddy.virtualHosts."http://auth.${hostName}.local".logFormat =
          lib.mkForce "output discard";
        services.caddy.virtualHosts."http://acp-ui.${hostName}.local".logFormat =
          lib.mkForce "output discard";
        services.caddy.virtualHosts."http://grafana.${hostName}.local".logFormat =
          lib.mkForce "output discard";
      }
    ];
  }).config;

  authSite = "http://auth.${hostName}.local";
  ak = config.lattice.authentik;
  grafanaSettings = config.services.grafana.settings or { };
in
assert config.lattice.authentik.enable;
assert config.lattice.authentik.listenAddress == "127.0.0.1";
# Caddy `auth` site proxies to the loopback listener.
assert lib.hasInfix "reverse_proxy" config.services.caddy.virtualHosts.${authSite}.extraConfig;
assert lib.hasInfix
  "${ak.listenAddress}:${toString ak.port}" config.services.caddy.virtualHosts.${authSite}.extraConfig;
# The backend port stays out of the firewall (loopback reachable from Caddy).
assert !(lib.elem ak.port config.networking.firewall.allowedTCPPorts);
# Per-service mDNS publisher for the `auth` alias exists.
assert builtins.hasAttr "auth-mdns" config.systemd.services;
assert lib.hasInfix "auth.${hostName}.local" config.systemd.services.auth-mdns.script;
# The publisher must enumerate ALL IPv4 uplinks — it must NOT pick a single
# address from the default route (`ip route get … src`), which on a dual-homed
# node silently republishes whichever interface currently holds the default
# route and kills the alias for clients on the other subnet (auth was published
# on the unreachable 192.168.3.12 instead of 192.168.60.184).
assert !(lib.hasInfix "route get 1.1.1.1" config.systemd.services.auth-mdns.script);
assert lib.hasInfix "addr show up" config.systemd.services.auth-mdns.script;
# Authentik runs as an unprivileged system user (wrapper skips root branch).
assert config.users.users.authentik.isSystemUser;
assert config.users.groups ? authentik;
# systemd units present and running as the non-root user.
assert builtins.hasAttr "authentik-server" config.systemd.services;
assert builtins.hasAttr "authentik-worker" config.systemd.services;
assert builtins.hasAttr "authentik-migrate" config.systemd.services;
assert config.systemd.services.authentik-server.serviceConfig.User == "authentik";
assert config.systemd.services.authentik-worker.serviceConfig.User == "authentik";
# The Rust worker must NOT share the server's loopback HTTP port (it binds its
# own healthcheck/metrics listeners itself and would fail with Address already
# in use), and its listen.http/listen.metrics must be valid non-empty socket
# addresses (an empty string crashes the Rust config parser with "invalid
# socket address syntax"). Both are enforced by workerEnv overriding these
# two keys with ephemeral loopback ports (127.0.0.1:0).
let
  serverEnv = config.systemd.services.authentik-server.environment;
  workerEnv = config.systemd.services.authentik-worker.environment;
in
assert serverEnv.AUTHENTIK_LISTEN__HTTP or "" != workerEnv.AUTHENTIK_LISTEN__HTTP or "";
assert workerEnv.AUTHENTIK_LISTEN__HTTP or "" == "127.0.0.1:0";
assert workerEnv.AUTHENTIK_LISTEN__METRICS or "" == "127.0.0.1:0";
# Every other listenEnv value stays intact for the worker (the Rust ListenConfig
# has no such fields; serde ignores them, Django tolerates them).
assert workerEnv.AUTHENTIK_LISTEN__HTTPS or "" == "";
# Migration one-shot must NOT invoke the buggy `ak manage migrate` (the
# wrapper's non-root branch injects a leading `manage`, so Django fails with
# `Unknown command: 'manage'` and the schema is never created → server/worker
# cannot start → Caddy 502). ExecStart is our migration runner (the
# writeShellScript that resolves the private python env and runs
# `python -m lifecycle.migrate`); it must neither be `ak` itself nor use the
# `ak ... manage` incantation.
let migrateExec = config.systemd.services.authentik-migrate.serviceConfig.ExecStart or "";
in
assert lib.hasInfix "authentik-migrate" migrateExec;
assert !(lib.hasInfix "manage" migrateExec);
assert !(lib.hasInfix "/bin/ak" migrateExec);
# Secrets are supplied via EnvironmentFile (runtime paths), never inline.
assert config.systemd.services.authentik-server.serviceConfig.EnvironmentFile or [ ] != null;
# ForwardAuth wraps the acp-ui browser UI (protected), status is untouched.
assert lib.hasInfix
  "forward_auth"
  config.services.caddy.virtualHosts."http://acp-ui.${hostName}.local".extraConfig;
# The forward_auth subrequest must hit the embedded outpost's forward-auth
# endpoint (/outpost.goauthentik.io/auth/caddy), which returns 302 → login for
# unauthenticated requests. The legacy /akprox/auth/ is NOT routed by this
# Authentik (answers 404) and broke every acp-ui request.
assert lib.hasInfix
  "/outpost.goauthentik.io/auth/caddy"
  config.services.caddy.virtualHosts."http://acp-ui.${hostName}.local".extraConfig;
assert !(lib.hasInfix "/akprox/"
  config.services.caddy.virtualHosts."http://acp-ui.${hostName}.local".extraConfig);
# Grafana native OIDC config is present when oauth is enabled (F14 step 7).
assert grafanaSettings."auth.generic_oauth".enabled or false;
assert grafanaSettings."auth.generic_oauth".client_id or "" == "grafana";
# Mesh site exists for auth (login page is NOT meshExcluded).
assert builtins.any
  (name: lib.hasSuffix "auth.homelab.myt.su" name)
  (builtins.attrNames config.services.caddy.virtualHosts);
# Caddy is the only external HTTP listener.
assert config.services.caddy.enable;
assert !config.services.nginx.enable;
assert builtins.elem 80 config.networking.firewall.allowedTCPPorts;
pkgs.runCommand "authentik-evaluation" {
  nativeBuildInputs = [ pkgs.caddy ];
} ''
  mkdir -p "$out"
  cp "${config.services.caddy.configFile}" "$out/caddyfile.in"
  echo "Authentik SSO contract holds" > "$out/result"
''
