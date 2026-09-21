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
  # Сервисы из meshExclude mesh-адрес НЕ получают — только LAN.
  # (Grafana выпущена на mesh, т.к. закрыта за Authentik SSO; llm-gateway без
  # публичной TLS/API-key защиты остаётся непубличным.)
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
  # F14: central SSO (Authentik) behind the same Caddy ingress. `auth` is the
  # login site; it must be reachable from both LAN and mesh (the login page is
  # the single entry point), so it is NOT added to meshExclude anywhere.
  authentikEnabled = config.lattice.authentik.enable or false;
  authentikCfg = config.lattice.authentik;

  # Публикует mDNS-алиас на КАЖДОМ реальном LAN-аплинке ноды. Ранее адрес для
  # avahi-publish брался один раз из маршрута по умолчанию (`ip route get
  # 1.1.1.1` → src) — на многодомной ноде (enp3s0 провода + wlp2s0 Wi-Fi, а то
  # и больше аплинков) это давало адрес того интерфейса, который сейчас держит
  # default-route, т.е. перезапуск юнита молча менял публикуемый адрес на
  # другой, недостижимый из клиентской подсети (см. инцидент с auth-mdns:
  # после F14 restart он уехал на 192.168.3.12, пока клиенты в 192.168.60.0/24
  # ждали 192.168.60.184). Решение оператора: распространять алиас по ВСЕМ
  # аплинкам, чтобы клиент из любой подсети разрешал релевантный для себя адрес
  # (mDNS живёт в пределах L2-сегмента). Исключаем loopback и туннели
  # (ygg0 — IPv6, в `ip -4` и так не попадёт; tun/tap/wg/br/veth/docker — не
  # реальные клиентские аплинки). Каждому адресу — свой процесс avahi-publish;
  # хоть один опубликованный адрес считается успехом.
  mdnsPublisher = service: {
    description = "Publish the ${service} mDNS alias on all LAN uplinks";
    wantedBy = [ "multi-user.target" ];
    after = [ "avahi-daemon.service" "network-online.target" ];
    requires = [ "avahi-daemon.service" ];
    wants = [ "network-online.target" ];
    script = ''
      alias=${lib.escapeShellArg (serviceHost service)}
      published=0
      while read -r iface addr; do
        [ -z "$addr" ] && continue
        ${config.services.avahi.package}/bin/avahi-publish --address --no-reverse "$alias" "$addr" \
          >> /dev/null 2>&1 &
        published=$((published + 1))
      done < <(
        ${pkgs.iproute2}/bin/ip -4 -o addr show up \
          | ${pkgs.gawk}/bin/awk '
            ! / lo / && $2 !~ /^(tun|tap|wg|br|veth|docker|virbr)/ {
              if (match($4, /^([0-9.]+)\/[0-9]+/, m)) print $2, m[1]
            }'
      )
      if [ "$published" -eq 0 ]; then
        echo "no usable IPv4 uplink to publish $alias on" >&2
        exit 1
      fi
      # Все процессы avahi-publish запущены в фоне; держим юнит живым, пока они
      # живы. Restart=always поднимет юнит заново, когда все упали.
      wait
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

    # F14: the Authentik login site. Proxied to its loopback listener by the
    # same serviceSites helper, so `auth.<node>.local` (LAN) and
    # `auth.<meshDomain>` (mesh) both reach the single loopback SSO. `auth` is
    # deliberately NOT in meshExclude — the login page must be reachable from
    # mesh clients (f14-01 step 4).
    (mkIf authentikEnabled (serviceSites "auth" ''
      reverse_proxy ${authentikCfg.listenAddress}:${toString authentikCfg.port}
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
    # что и в hostname: "llm-gateway", ...). Нужно для сервисов,
    # которые по политике нельзя открывать извне (llm-gateway — нет
    # TLS/API-key защиты).
    meshExclude = mkOption {
      type = types.listOf types.str;
      default = [ ];
      example = [ "llm-gateway" ];
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
        # по HTTP-01/TLS-ALPN. Секрет подаётся через services.caddy.environmentFile
        # (переменная CLOUDFLARE_API_TOKEN), откуда Caddy подставляет её через
        # placeholder {env.CLOUDFLARE_API_TOKEN} в субдирективу api_token
        # (caddy-dns/cloudflare v0.2.4 принимает только api_token/zone_token,
        # а не env — см. UnmarshalCaddyfile плагина).
        acme_dns cloudflare {
          api_token {env.CLOUDFLARE_API_TOKEN}
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
      (mkIf authentikEnabled {
        # F14: publish the `auth` mDNS alias so `auth.<node>.local` resolves.
        auth-mdns = mdnsPublisher "auth";
      })
    ];
  };
}
