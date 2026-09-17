{ config, lib, pkgs, ... }:

let
  inherit (lib) mkOption mkIf types optionalAttrs;
  cfg = config.lattice.tcp-gateway;

  hostName = config.networking.hostName;
  serviceHost = service: "${service}.${hostName}.local";
  siteAddress = service: "http://${serviceHost service}";

  # f4-05: внешний (mesh) ingress. Каждый активный сервис получает параллельно
  # LAN-адресу `http://<svc>.<node>.local/` address `http(s)://<svc>.<meshDomain>/`,
  # указывающий на тот же backend. Когда оператор предоставит Cloudflare-токен
  # (DNS-01, acme_dns), mesh-сайты обслуживаются по HTTPS с автоматическим
  # сертификатом; без токена mesh остаётся на plain HTTP через тот же :80.
  # Сервисы из meshExclude mesh-адрес НЕ получают — только LAN (например,
  # grafana/llm-gateway обязаны остаться непубличными).
  meshEnabled = cfg.meshDomain != null;
  enableCloudflare = cfg.cloudflareToken != null;
  meshScheme = if enableCloudflare then "https" else "http";
  meshAddress = service: "${meshScheme}://${service}.${cfg.meshDomain}";
  meshExcluded = service: lib.elem service cfg.meshExclude;

  # Строит набор Caddy virtualHosts для сервиса: LAN-сайт всегда, mesh-сайт
  # только если meshDomain задан И сервис не в meshExclude. backend — значение
  # extraConfig vitualHost.
  serviceSites = service: extraConfig:
    { ${siteAddress service} = { inherit extraConfig; }; }
    // optionalAttrs (meshEnabled && !(meshExcluded service)) {
      ${meshAddress service} = { inherit extraConfig; };
    };

  radicleEnabled =
    (config.services.radicle.enable or false) && (config.services.radicle.httpd.enable or false);
  rnsServerEnabled =
    (config.lattice.rns-server.enable or false) &&
    (config.lattice.rns-server.server.http.enabled or false) &&
    (config.lattice.rns-server.server.http.port or null != null);
  llmGatewayEnabled = config.lattice.llm-gateway.enable or false;
  piAcpEnabled = config.lattice.pi-acp-daemon.enable or false;
  piAcpCfg = config.lattice.pi-acp-daemon;
  gitCacheProxyEnabled = config.lattice.git-cache-proxy.enable or false;
  gitCacheProxyCfg = config.lattice.git-cache-proxy;
  grafanaEnabled = config.lattice.grafana.enable or false;
  grafanaCfg = config.lattice.grafana;

  mdnsPublisher = service: {
    description = "Publish the ${service} mDNS alias";
    wantedBy = [ "multi-user.target" ];
    after = [ "avahi-daemon.service" "network-online.target" ];
    requires = [ "avahi-daemon.service" ];
    wants = [ "network-online.target" ];
    script = ''
      address="$(${pkgs.iproute2}/bin/ip -4 -o route get 1.1.1.1 \
        | ${pkgs.gawk}/bin/awk '{ for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }')"
      if [ -z "$address" ]; then
        echo "could not determine the primary IPv4 address" >&2
        exit 1
      fi
      exec ${config.services.avahi.package}/bin/avahi-publish \
        --address --no-reverse ${lib.escapeShellArg (serviceHost service)} "$address"
    '';
    serviceConfig = {
      Restart = "always";
      RestartSec = 5;
    };
  };

  # Автоматически собираем проксирование для активных TCP/HTTP сервисов.
  # Каждая запись оборачивается serviceSites: LAN + mesh, один и тот же backend.
  proxiedServices = lib.mkMerge [
    # 1. Radicle HTTP Gateway
    (mkIf radicleEnabled (serviceSites "radicle" ''
      reverse_proxy 127.0.0.1:${toString config.services.radicle.httpd.listenPort}
    ''))

    # 2. RNS Server HTTP Control Plane
    (mkIf rnsServerEnabled (serviceSites "rns-server" ''
      reverse_proxy 127.0.0.1:${toString config.lattice.rns-server.server.http.port}
    ''))

    # 3. OpenAI-compatible LLM Gateway via its mDNS alias.
    (mkIf llmGatewayEnabled (serviceSites "llm-gateway" ''
      reverse_proxy ${config.lattice.llm-gateway.host}:${toString config.lattice.llm-gateway.port}
    ''))

    # 4. Authenticated LAN WebSocket ingress for the loopback-only Pi ACP daemon.
    (mkIf piAcpEnabled (serviceSites piAcpCfg.serviceName ''
      handle {
        rewrite * /acp?token=${piAcpCfg.internalToken}
        reverse_proxy ${piAcpCfg.host}:${toString piAcpCfg.port} {
          header_up -Authorization
        }
      }
    ''))

    # 5. LAN ingress for the loopback-only Git cache proxy (F9).
    # The proxy is a shared credentialed reader; "can reach this host" ==
    # "can read every mirrored repo", so it is exposed only through the
    # operator-controlled Caddy ingress on the LAN.
    (mkIf gitCacheProxyEnabled (serviceSites "git-cache-proxy" ''
      reverse_proxy ${gitCacheProxyCfg.host}:${toString gitCacheProxyCfg.port}
    ''))

    # 6. LAN ingress for the loopback-only Grafana frontend (F12). Grafana
    # binds 127.0.0.1 by design (non-public); this Caddy site is how an
    # operator reaches it from the LAN, `http://grafana.<node>.local/`. The
    # admin login is still gated by the agenix-backed admin password.
    (mkIf grafanaEnabled (serviceSites "grafana" ''
      reverse_proxy ${grafanaCfg.listenAddress}:${toString grafanaCfg.port}
    ''))
  ];
in
{
  options.lattice.tcp-gateway = {
    # f4-05: external (mesh) domain served by Caddy in parallel to the LAN
    # *.local contract. When set, every enabled service additionally gets
    # `http(s)://<service>.<meshDomain>/` routing to the same backend. DNS for
    # `*.<meshDomain>` (AAAA records) points at the node's Yggdrasil address
    # (see nodes/mytecor-homelab/README.md). null → mesh ingress disabled.
    meshDomain = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "homelab.myt.su";
      description = ''
        External domain on which the same services are served alongside the LAN
        `.local` addresses. Clients reach them from the Yggdrasil mesh (the DNS
        records resolve to the node's 200::/7 address). Leave null to keep the
        profile LAN-only.
      '';
    };

    # f4-05: сервисы, которые на mesh-адресе НЕ выпускаются (остаются только
    # на LAN-контракте *.local). Оператор задаёт сетевое имя сервиса (то же,
    # что и в hostname: "grafana", "llm-gateway", ...). Нужно для сервисов,
    # которые по политике нельзя открывать извне (grafana/llm-gateway — нет
    # TLS/API-key защиты).
    meshExclude = mkOption {
      type = types.listOf types.str;
      default = [ ];
      example = [ "grafana" "llm-gateway" ];
      description = ''
        Services that are NOT exposed on the mesh (external) address and keep
        only their LAN `.local` site. Useful for services that must stay
        non-public (no TLS / no API-key protection).
      '';
    };

    # f4-05: path (agenix-decrypted runtime file) containing CLOUDFLARE_API_TOKEN.
    # When set, Caddy is built with the cloudflare DNS plugin and the global
    # `acme_dns cloudflare` block is added, so mesh hosts get HTTPS certs via
    # DNS-01 (validation through the public DNS zone — required because the
    # mesh AAAA records are not reachable by public CA servers over HTTP-01).
    # null → no TLS, mesh stays on plain HTTP via :80.
    cloudflareToken = mkOption {
      type = types.nullOr types.path;
      default = null;
      example = "/run/agenix/caddy-cloudflare-token";
      description = ''
        Runtime path to a file containing `CLOUDFLARE_API_TOKEN=...`, used as
        Caddy's systemd EnvironmentFile and referenced from the acme_dns block.
      '';
    };
  };

  config = {
    services.caddy = {
      enable = true;
      virtualHosts = proxiedServices;

      # f4-05: only with the Cloudflare token do we need the custom plugin build
      # and the DNS01 ACME configuration.
      package = mkIf enableCloudflare (pkgs.caddy.withPlugins {
        plugins = [ "github.com/caddy-dns/cloudflare@v0.2.4" ];
        hash = "sha256-dQvk6ezY6TQ1J7PjhCXnThF/SqVgPwBO8/RXzHCY+js=";
      });
      environmentFile = mkIf enableCloudflare cfg.cloudflareToken;
      globalConfig = mkIf enableCloudflare ''
        # f4-05: DNS-01 через Cloudflare — сертификаты для mesh-сайтов
        # (*.homelab.myt.su) валидируются через публичную зону DNS, т.к. их
        # AAAA-записи ведут на yggdrasil-адрес и для публичных CA недостижимы
        # по HTTP-01/TLS-ALPN. Токен берётся из секрета (services.caddy.environmentFile).
        acme_dns cloudflare {
          env CLOUDFLARE_API_TOKEN
        }
      '';
    };

    # :80 всегда (LAN + mesh по HTTP); :443 — только когда выдан Cloudflare-токен (HTTPS).
    networking.firewall.allowedTCPPorts =
      [ 80 ]
      ++ (if enableCloudflare then [ 443 ] else []);

    services.avahi = {
      enable = true;
      publish = {
        enable = true;
        addresses = true;
        userServices = true;
      };
    };

    # Avahi publishes only the node's primary hostname by default. Keep one
    # publisher per active service so every Caddy host resolves through mDNS.
    systemd.services = lib.mkMerge [
      (mkIf radicleEnabled { radicle-mdns = mdnsPublisher "radicle"; })
      (mkIf rnsServerEnabled { rns-server-mdns = mdnsPublisher "rns-server"; })
      (mkIf llmGatewayEnabled { llm-gateway-mdns = mdnsPublisher "llm-gateway"; })
      (mkIf piAcpEnabled {
        "${piAcpCfg.serviceName}-mdns" = mdnsPublisher piAcpCfg.serviceName;
      })
      (mkIf gitCacheProxyEnabled {
        git-cache-proxy-mdns = mdnsPublisher "git-cache-proxy";
      })
      (mkIf grafanaEnabled {
        grafana-mdns = mdnsPublisher "grafana";
      })
    ];
  };
}
