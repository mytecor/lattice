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
#     loopback without a rule;
#   - the LAN and mesh acp-ui sites both ForwardAuth-wrap with the standard
#     /outpost.goauthentik.io/auth/caddy endpoint and do NOT pin the subrequest
#     host to LAN (so a per-host mesh provider in Authentik can match — otherwise
#     the mesh site returns an Authentik 404 page instead of the static SPA).

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
        # domain is mesh-canonical (full https URL) and the OIDC endpoints use
        # the mesh auth host, mirroring the production node: root_url drives
        # the OIDC callback and must be the external host, not the loopback.
        lattice.grafana = {
          enable = true;
          port = 3000;
          adminPasswordFile = "/run/agenix/grafana-admin-password";
          secretKeyFile = "/run/agenix/grafana-secret-key";
          domain = "https://grafana.homelab.myt.su";
          oauth = {
            clientId = "grafana";
            clientSecretFile = "/run/agenix/grafana-oauth-client-secret";
            authUrl = "https://auth.homelab.myt.su/application/o/authorize/";
            tokenUrl = "https://auth.homelab.myt.su/application/o/token/";
            apiUrl = "https://auth.homelab.myt.su/application/o/userinfo/";
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
        lattice.authentik.oidcApplications = [
          {
            service = "grafana";
            clientId = "grafana";
            clientSecretFile = "/run/agenix/grafana-oauth-client-secret";
            callbackPath = "/login/generic_oauth";
          }
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
# Each publisher must launch avahi-publish in the BACKGROUND (&) inside the
# loop: avahi-publish is long-running, so a foreground call would block the
# while-loop on the first uplink and silently drop every later address (this
# caused ALL aliases to publish only 192.168.3.12 after the F14-aera deploy).
assert lib.hasInfix "2>&1 &" config.systemd.services.auth-mdns.script;
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
# f14 fix (mesh 404): the mesh acp-ui site must ALSO be ForwardAuth-wrapped with
# the same standard endpoint. Authentik's outpost matches the app by
# X-Forwarded-Host/Host against the provider's external_host (one provider per
# host); the mesh site presents its own host, so the generated Blueprint's
# per-host mesh provider is what makes the mesh host match instead of 404. The
# wrapper must be present on the mesh site too.
assert lib.hasInfix
  "forward_auth"
  config.services.caddy.virtualHosts."https://acp-ui.homelab.myt.su".extraConfig;
assert lib.hasInfix
  "/outpost.goauthentik.io/auth/caddy"
  config.services.caddy.virtualHosts."https://acp-ui.homelab.myt.su".extraConfig;
# …and it must not pin the subrequest host to the LAN host (else the mesh
# provider could never match).
assert !(lib.hasInfix "X-Forwarded-Host"
  config.services.caddy.virtualHosts."https://acp-ui.homelab.myt.su".extraConfig);
# Provider/application/outpost state is a native Authentik Blueprint generated
# by NixOS. It is applied after migrations and before server/worker/Caddy, then
# remains in Authentik's discovery directory for periodic reconciliation.
assert builtins.hasAttr "authentik-applications-blueprint" config.systemd.services;
let
  blueprintUnit = config.systemd.services.authentik-applications-blueprint;
  blueprintExec = blueprintUnit.serviceConfig.ExecStart or [ ];
  blueprintPath = blueprintUnit.environment.LATTICE_AUTHENTIK_APPLICATIONS_BLUEPRINT or "";
in
assert builtins.elem "authentik-migrate.service" (blueprintUnit.requires or [ ]);
assert builtins.elem "authentik-server.service" (blueprintUnit.before or [ ]);
assert builtins.elem "authentik-worker.service" (blueprintUnit.before or [ ]);
assert builtins.elem "caddy.service" (blueprintUnit.before or [ ]);
assert lib.length blueprintExec == 3;
assert lib.all (lib.hasInfix "/bin/ak apply_blueprint") blueprintExec;
assert lib.any (lib.hasInfix "flow-default-provider-authorization-explicit-consent.yaml") blueprintExec;
assert lib.any (lib.hasInfix "flow-default-provider-invalidation.yaml") blueprintExec;
assert lib.any (lib.hasInfix "/lattice/applications.yaml") blueprintExec;
assert lib.hasSuffix "/lattice/applications.yaml" blueprintPath;
assert config.systemd.services.authentik-worker.environment.AUTHENTIK_BLUEPRINTS_DIR or "" != "";
assert builtins.elem "authentik-applications-blueprint.service"
  (config.systemd.services.caddy.requires or [ ]);
# Grafana native OIDC config is present when oauth is enabled (F14 step 7).
assert grafanaSettings."auth.generic_oauth".enabled or false;
assert grafanaSettings."auth.generic_oauth".client_id or "" == "grafana";
assert grafanaSettings."auth.generic_oauth".auth_style or "" == "InHeader";
# F14/F4-05 (mesh): Grafana is closed behind Authentik via native OIDC and is
# exposed on the public mesh. The mesh site must exist (grafana NOT meshExcluded)
# and root_url must be the mesh-canonical external https URL (it drives the OIDC
# callback /login/generic_oauth), not the loopback listener.
assert grafanaSettings.server.root_url or "" == "https://grafana.homelab.myt.su/";
assert builtins.hasAttr "https://grafana.homelab.myt.su" config.services.caddy.virtualHosts;
# Grafana itself can advertise only one static (mesh-canonical) OAuth URL. The
# LAN Caddy site must therefore rewrite the browser-facing authorize redirect,
# its encoded redirect_uri, and Grafana's absolute return redirects to *.local.
# The mesh site must remain untouched.
let
  grafanaLanConfig =
    config.services.caddy.virtualHosts."http://grafana.${hostName}.local".extraConfig;
  grafanaMeshConfig =
    config.services.caddy.virtualHosts."https://grafana.homelab.myt.su".extraConfig;
in
assert lib.hasInfix
  "header_down Location https://auth[.]homelab[.]myt[.]su http://auth.${hostName}.local"
  grafanaLanConfig;
assert lib.hasInfix
  "https%3A%2F%2Fgrafana[.]homelab[.]myt[.]su http%3A%2F%2Fgrafana.${hostName}.local"
  grafanaLanConfig;
assert lib.hasInfix
  "https://grafana[.]homelab[.]myt[.]su http://grafana.${hostName}.local"
  grafanaLanConfig;
assert !(lib.hasInfix "header_down Location" grafanaMeshConfig);
# OIDC endpoints use the mesh auth host (reachable from LAN and ygg clients);
# the same backend as LAN auth.<node>.local.
assert grafanaSettings."auth.generic_oauth".auth_url or "" == "https://auth.homelab.myt.su/application/o/authorize/";
assert grafanaSettings."auth.generic_oauth".token_url or "" == "https://auth.homelab.myt.su/application/o/token/";
assert grafanaSettings."auth.generic_oauth".api_url or "" == "https://auth.homelab.myt.su/application/o/userinfo/";
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
  inherit blueprintPath;
  blueprintsDir = config.systemd.services.authentik-worker.environment.AUTHENTIK_BLUEPRINTS_DIR;
} ''
  mkdir -p "$out"
  cp "${config.services.caddy.configFile}" "$out/caddyfile.in"
  test "$(find "$blueprintsDir" -type l -print -quit)" = ""
  test ! -L "$blueprintPath"
  grep -F 'model: authentik_providers_proxy.proxyprovider' "$blueprintPath"
  grep -F 'external_host: "http://acp-ui.${hostName}.local"' "$blueprintPath"
  grep -F 'external_host: "https://acp-ui.homelab.myt.su"' "$blueprintPath"
  grep -F 'model: authentik_outposts.outpost' "$blueprintPath"
  grep -A6 'slug: "acp-ui-fa"' "$blueprintPath" | grep -F 'name: "acp-ui"'
  grep -A6 'slug: "acp-ui-fa"' "$blueprintPath" | grep -F 'meta_hide: false'
  # The VISIBLE LAN card must launch the LAN URL, not the mesh URL.
  # (Regression: canonical-launch logic preferred the mesh host, so the card
  # opened https://acp-ui.homelab.myt.su from the LAN UI.) -F with the closing
  # quote keeps the "/slug/" from matching the "-mesh" slug.
  grep -A5 -F 'slug: "acp-ui-fa"' "$blueprintPath" | grep -F 'meta_launch_url: "http://acp-ui.${hostName}.local/"'
  if grep -A5 -F 'slug: "acp-ui-fa"' "$blueprintPath" | grep -F 'meta_launch_url: "https://acp-ui.homelab.myt.su/"'; then
    echo "LAN app must not launch the mesh URL" >&2
    exit 1
  fi
  grep -A6 'slug: "acp-ui-fa-mesh"' "$blueprintPath" | grep -F 'meta_hide: true'
  grep -F 'model: authentik_providers_oauth2.oauth2provider' "$blueprintPath"
  grep -F 'client_id: "grafana"' "$blueprintPath"
  grep -F 'client_secret: !File "/run/agenix/grafana-oauth-client-secret"' "$blueprintPath"
  grep -F 'url: "http://grafana.${hostName}.local/login/generic_oauth"' "$blueprintPath"
  grep -F 'url: "https://grafana.homelab.myt.su/login/generic_oauth"' "$blueprintPath"
  echo "Authentik SSO contract holds" > "$out/result"
''
