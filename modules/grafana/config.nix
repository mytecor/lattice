{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.grafana;

  # datasources are provisioned (no hand-editing): Prometheus and Loki, both
  # loopback, both proxied by Grafana (access=proxy) so users never see the
  # storage hosts. No credentials needed for either on loopback.
  datasources = [
    {
      name = "Prometheus";
      type = "prometheus";
      access = "proxy";
      uid = "prometheus";
      url = cfg.prometheusUrl;
      isDefault = true;
      jsonData = {
        httpMethod = "GET";
      };
    }
    {
      name = "Loki";
      type = "loki";
      access = "proxy";
      uid = "loki";
      url = cfg.lokiUrl;
    }
  ];

  # Dashboard provider: a provisioning "providers" entry that points Grafana
  # at the module's own dashboards directory (modules/grafana/dashboards/).
  # Dashboard definitions live in the repository, not hand-edited in the UI.
  #
  # Operators extend the fleet through cfg.dashboardProviders, which replaces
  # the default rather than appends to it: the repository is the single source
  # of truth and there is exactly one provider set (f12-04). The module ships
  # the LLM Gateway + Gateway runtime dashboards by default.
  dashboardProvider = {
    name = "lattice";
    folder = "Lattice";
    options = {
      path = "${./dashboards}";
    };
  };

  # The public-facing external URL Grafana advertises. `root_url` drives the
  # OIDC callback (`/login/generic_oauth`) and every absolute link Grafana
  # emits, so it must match the host the operator's browser actually uses —
  # never the loopback listener. `domain` is either a bare host (legacy form:
  # root_url http://<domain>:<port>/, the pre-SSO default) or a full URL with
  # scheme (canonical for SSO/edge, e.g. https://grafana.homelab.myt.su).
  externalRootUrl =
    if lib.hasPrefix "http://" cfg.domain || lib.hasPrefix "https://" cfg.domain
    then "${cfg.domain}/"
    else "http://${cfg.domain}:${toString cfg.port}/";

  # F14: OIDC sign-in through Authentik is on only when a client secret is
  # supplied (an agenix runtime path). Without it, the admin-password login
  # stays the only way in.
  oauthEnabled = cfg.oauth.clientSecretFile != null;
in
{
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.adminPasswordFile != null;
        message = ''
          lattice.grafana: adminPasswordFile must be set (an agenix secret with
          the admin password). Refusing to run with the default password or a
          plaintext store value. Provide
          lattice.grafana.adminPasswordFile = config.age.secrets.<name>.path.
        '';
      }
      {
        assertion = cfg.secretKeyFile != null;
        message = ''
          lattice.grafana: secretKeyFile must be set (NixOS 26.05 removed the
          Grafana secret_key default). Generate with `openssl rand -hex 32`,
          store in agenix, and point lattice.grafana.secretKeyFile at it.
        '';
      }
    ];

    services.grafana = {
      enable = true;
      package = cfg.package;
      dataDir = toString cfg.dataDir;
      settings =
        {
          server = {
            http_addr = cfg.listenAddress;
            http_port = cfg.port;
            domain = cfg.domain;
            root_url = externalRootUrl;
          };
          security = {
            admin_user = cfg.adminUser;
            # File provider: Grafana expands $__file{<path>} to the trimmed file
            # contents at startup. The value never appears in the Nix store
            # (nixpkgs grafana module warns otherwise).
            admin_password = "$__file{${toString cfg.adminPasswordFile}}";
            # NixOS 26.05 requires an explicit secret_key (no default); supplied
            # the same way, never plaintext in the store.
            secret_key = "$__file{${toString cfg.secretKeyFile}}";
            # F12: anonymous/gravatar disabled; no analytics reporting.
            disable_gravatar = true;
          };
          analytics = {
            reporting_enabled = false;
            check_for_updates = false;
          };
          users = {
            allow_sign_up = false;
          };
        }
        // lib.optionalAttrs oauthEnabled {
          # F14: native OIDC sign-in through the central Authentik — the single
          # entry point. client_secret read via the file provider ($__file{...})
          # from an agenix path, never in the store. The admin role is mapped
          # from the Authentik operator group via role_attribute_path.
          "auth.generic_oauth" = {
            name = cfg.oauth.name;
            enabled = true;
            client_id = cfg.oauth.clientId;
            client_secret = "$__file{${toString cfg.oauth.clientSecretFile}}";
            auth_url = cfg.oauth.authUrl;
            token_url = cfg.oauth.tokenUrl;
            api_url = cfg.oauth.apiUrl;
            auth_style = cfg.oauth.authStyle;
            scopes = lib.concatStringsSep " " cfg.oauth.scopes;
            role_attribute_path =
              "contains(groups[*], '${cfg.oauth.adminGroup}') && 'Admin' || 'Viewer'";
            role_attribute_strict = true;
            # Authentik signs the ID token; exchange with PKCE.
            use_pkce = true;
            allow_sign_up = true;
          };
        };
      provision = {
        datasources.settings.datasources = datasources;
        dashboards.settings.providers =
          if cfg.dashboardProviders != [ ]
          then cfg.dashboardProviders
          else [ dashboardProvider ];
      };
    };

    # nixpkgs grafana already hardens (ProtectSystem=full, NoNewPrivileges,
    # CapabilityBoundingSet="" on non-privileged ports, loopback http_addr by
    # default). Nothing to override.
  };
}
