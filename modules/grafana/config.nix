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
  # at a directory of dashboard JSON files shipped by the module. The f12-04
  # dashboard JSON lives under modules/grafana/dashboards/.
  dashboardProvider = {
    name = "lattice";
    folder = "Lattice";
    options = {
      path = "${./dashboards}";
    };
  };
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
      settings = {
        server = {
          http_addr = cfg.listenAddress;
          http_port = cfg.port;
          domain = cfg.domain;
          root_url = "http://${cfg.domain}:${toString cfg.port}/";
        };
        security = {
          admin_user = cfg.adminUser;
          # File provider: Grafana expands ${__file:<path>} to the contents at
          # startup. The value never appears in the Nix store (nixpkgs grafana
          # module warns otherwise).
          admin_password = "\${__file:${toString cfg.adminPasswordFile}}";
          # NixOS 26.05 requires an explicit secret_key (no default); supplied
          # the same way, never plaintext in the store.
          secret_key = "\${__file:${toString cfg.secretKeyFile}}";
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
      };
      # Provision datasources (Prometheus + Loki) and the dashboard provider.
      provision = {
        datasources.settings.datasources = datasources;
        dashboards.settings.providers = [ dashboardProvider ];
      };
    };

    # nixpkgs grafana already hardens (ProtectSystem=full, NoNewPrivileges,
    # CapabilityBoundingSet="" on non-privileged ports, loopback http_addr by
    # default). Nothing to override.
  };
}
