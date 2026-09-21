{
  lib,
  pkgs,
  ...
}:

let
  inherit (lib) mkEnableOption mkOption types;
in
{
  options.lattice.authentik = {
    enable = mkEnableOption "the Lattice Authentik SSO server (central user identity)";

    package = mkOption {
      type = types.package;
      default = pkgs.authentik;
      defaultText = lib.literalExpression "pkgs.authentik";
      description = "The Authentik package to run (provides the `ak` wrapper).";
    };

    # Authentik binds 127.0.0.1 by design (F14): it stands behind the single
    # external Caddy ingress (profiles/tcp-gateway). LAN/mesh reachability is
    # the operator-controlled `auth` Caddy site, never a direct listener.
    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = "Loopback address Authentik binds (non-public).";
    };

    port = mkOption {
      type = types.port;
      description = "Loopback TCP port Authentik serves HTTP on.";
    };

    # Public-facing hostname for Authentik (affects the redirect/root URL).
    domain = mkOption {
      type = types.str;
      default = "localhost";
      description = "Public-facing domain for Authentik's web UI (externally routed by Caddy).";
    };

    # The postgres role Authentik uses. The module creates this OS-level system
    # user with the same name; nixpkgs postgresql's default peer auth over the
    # unix socket authenticates it without any password (see modules/f14).
    dbUser = mkOption {
      type = types.str;
      default = "authentik";
      description = "PostgreSQL role (and OS system user) owning the Authentik database.";
    };

    dataDir = mkOption {
      type = types.path;
      default = "/var/lib/authentik";
      description = "Persistent Authentik data directory. Survives reboots via /persist.";
    };

    # Secret files arrive as agenix runtime paths (never in the Nix store).
    # Each names the AUTHENTIK_* variable Authentik reads at startup; the
    # systemd units load them through EnvironmentFile, so values never appear
    # in argv, the store, or the module's generated text. The file for a
    # *_password must be readable before postgresql peer-auth fallback.
    secretKeyFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (agenix secret) to a file containing
        `AUTHENTIK_SECRET_KEY=<secret>`. Authentik's Django SECRET_KEY; required
        (no default) — the module refuses to run without it.
      '';
    };

    bootstrapTokenFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (agenix secret) to a file containing
        `AUTHENTIK_BOOTSTRAP_TOKEN=<token>`. Authentik's bootstrap token used
        by the built-in proxy/outpost bootstrap flow. Consumed via systemd
        EnvironmentFile, never in the store.
      '';
    };

    bootstrapUserFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (agenix secret) to a file containing
        `AUTHENTIK_BOOTSTRAP_USERNAME=<name>`. Initial operator account username.
      '';
    };

    bootstrapEmailFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (agenix secret) to a file containing
        `AUTHENTIK_BOOTSTRAP_EMAIL=<email>`. Initial operator account email.
      '';
    };

    bootstrapPasswordFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      description = ''
        Runtime path (agenix secret) to a file containing
        `AUTHENTIK_BOOTSTRAP_PASSWORD=<password>`. Initial operator account
        password (AUTHENTIK_BOOTSTRAP_PASSWORD).
      '';
    };

    # F14 step 6: Caddy ForwardAuth for browser-facing sites that have no SSO
    # of their own. Each entry names an existing Caddy site (a `serviceSites`
    # host) that is *not* touched at the backend: the site's extraConfig is
    # wrapped with a Caddy `forward_auth` against Authentik's loopback
    # /outpost.goauthentik.io/auth/caddy subrequest endpoint, so only the
    # browser UI is gated. API endpoints of a service with split UI/API are
    # reached through their own (unwrapped) site.
    forwardAuth = mkOption {
      type = types.listOf (types.submodule {
        options = {
          # Caddy site/virtualHost name, e.g. "acp-ui" (matches the serviceSites
          # service name: acp-ui.<node>.local and acp-ui.<meshDomain>).
          service = mkOption {
            type = types.str;
            description = "Service/site name to wrap with Authentik ForwardAuth (browser UI only).";
          };

          # Authentik flow executor to use for the login redirect. The operator
          # provisions a default flow via the repository blueprint; this is the
          # well-known identifier of that flow.
          flows = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Authentik flow slugs that may (re)start for this site's unauthenticated requests.";
          };

          # Optional path prefix (e.g. "/ui") to add to the auth_request URI so
          # that a service with split UI/API only protects its UI subpath while
          # the API paths stay open (f14-01 step 8).
          uriPrune = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "Optional UI subpath prefix used for the forward_auth request (uri_prune).";
          };
        };
      });
      default = [ ];
      description = ''
        Browser-facing sites to wrap with Authentik Caddy ForwardAuth. The
        backend is untouched: only the site's extraConfig wraps a
        `forward_auth` + `auth_request` against the Authentik forward-auth
        subrequest /outpost.goauthentik.io/auth/caddy on the loopback
        Authentik. Set the `lattice.tcp-gateway` ingress to expose `auth` and
        enable this list to protect each named site's browser UI.
      '';
    };

    # Low-level overrides (rarely needed); kept minimal.
    logLevel = mkOption {
      type = types.str;
      default = "info";
      description = "Authentik log level (info/warning/error/debug).";
    };
  };
}
