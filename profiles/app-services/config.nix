{ config, lib, pkgs, ... }:

let
  hostName = config.networking.hostName;
  statusHost = "status.${hostName}.local";
  latticePorts = import ../networking/ports.nix;
  statusAddress = "127.0.0.1";
  statusPort = latticePorts.node-status;

  # f4-05: mesh (внешний) доступ к status — параллельно LAN-контракту. Читаем те же
  # опции, что задаёт профиль tcp-gateway: meshDomain и cloudflareToken. Без токена
  # mesh-сайт обслуживается по plain HTTP (:80); с токеном — по HTTPS (acme_dns).
  meshDomain = config.lattice.tcp-gateway.meshDomain;
  meshCloudflare = config.lattice.tcp-gateway.cloudflareToken != null;
  meshScheme = if meshCloudflare then "https" else "http";
  statusMeshHost = if meshDomain != null then "${meshScheme}://status.${meshDomain}" else null;
  # f13-01 (mesh, 2026-09-20): acp-ui получает HTTPS на mesh-домене так же, как
  # и остальные сервисы (см. serviceSites в tcp-gateway). Раньше сайт существовал
  # только на LAN-контракте http://acp-ui.<node>.local, и на https://acp-ui.<meshDomain>
  # у Caddy не было ни сайта, ни сертификата — TLS-хендшейк падал ("SSL не работает").
  # Без токена (meshCloudflare) mesh-сайт обслуживается по plain HTTP через тот же :80.
  acpUiMeshHost = if meshDomain != null then "${meshScheme}://acp-ui.${meshDomain}" else null;

  # LAN и mesh проксируют один loopback-only Go backend.
  statusSiteConfig = ''
    @metrics path /metrics
    respond @metrics 404
    reverse_proxy ${statusAddress}:${toString statusPort}
  '';
  cominSourceRepo = "/var/lib/comin/source/repository";

  # f13-01: web-клиент ACP (acp-components) как статический SPA. Обслуживается
  # Caddy file_server из store-path пакета packages/acp-web; mDNS-alias
  # acp-ui.<node>.local публикуется avahi-сервисом ниже, а на mesh-домене
  # (f13-01 mesh, 2026-09-20) тот же контент доступен по https://acp-ui.<meshDomain>.
  # Клиент подключается к существующему ACP ingress ws(s)://acp.<host>/ (f8-06),
  # выводя host из своего собственного (см. patch-main-ts.mjs).
  acpUiHost = "acp-ui.${hostName}.local";
  acpWebPkg = pkgs.lattice.acp-web;
  # SPA: все пути, кроме реальных файлов, отдаём index.html (клиентская
  # маршрутизация); file_server поверх store-каталога пакета.
  acpUiSiteConfig = ''
    root * ${acpWebPkg}
    try_files {path} /index.html
    file_server
  '';

  # F14: Authentik Caddy ForwardAuth for browser-facing sites without their own
  # SSO. The wrap prepends `forward_auth` against the loopback Authentik before
  # the service's own handlers, gating only the browser UI — the backend
  # contract (API/ws) is untouched. Which sites get wrapped is decided by the
  # node via lattice.authentik.forwardAuth (source of truth), not hardcoded
  # here.
  # Defensive: app-services runs in tests/nodes that may not import the
  # authentik module at all; treat that as "SSO off" rather than erroring.
  authentikCfg = config.lattice.authentik or {
    enable = false;
    forwardAuth = [ ];
    listenAddress = "127.0.0.1";
    port = 9220;
  };
  forwardAuthEntry = service:
    lib.findFirst (e: e.service == service) null (authentikCfg.forwardAuth or [ ]);
  # Wrap a site's extraConfig with Authentik forward_auth when the service is
  # listed in lattice.authentik.forwardAuth; otherwise return it unchanged.
  wrapForwardAuth = service: baseConfig:
    let
      # F14: Authentik's forward-auth subrequest path. The loopback server's
      # embedded outpost serves it at /outpost.goauthentik.io/auth/caddy (it
      # detects the target app from the X-Forwarded-* headers / Host), NOT the
      # legacy /akprox/auth/ which Django no longer routes and answers 404 —
      # that 404 made Caddy's forward_auth treat every acp-ui request as
      # denied. Default matches the documented Caddy integration for this
      # Authentik (2026.5.6). A node may override per service via uri.
      entry = forwardAuthEntry service;
      uri = entry.uri or "/outpost.goauthentik.io/auth/caddy";
    in
    if entry == null || !authentikCfg.enable then
      baseConfig
    else
      ''
        # F14: Authentik SSO gate (browser UI only). 401/302 → Authentik login.
        forward_auth ${authentikCfg.listenAddress}:${toString authentikCfg.port} {
            uri ${uri}
        }

        ${baseConfig}
      '';

  # f13-01: публикация service-specific mDNS alias (avahi-publish --address)
  # — тот же приём, что у статус-сайта; параметризуем, чтобы не дублировать
  # юнит. Алias отдельного имения убирает необходимость в Host-заголовке и
  # дополнительной DNS-записи на стороне клиента.
  #
  # f14-fix: алиас публикуется на КАЖДОМ реальном LAN-аплинке ноды (а не на
  # адресе из маршрута по умолчанию — на многодомной ноде default-route
  # менявшийся после F14 restart уводил auth-mdns на недостижимый 192.168.3.12
  # вместо 192.168.60.184). См. mdnsPublisher в profiles/tcp-gateway/config.nix —
  # здесь тот же подход.
  mdnsPublishService = alias: {
    description = "Publish the ${alias} mDNS alias on all LAN uplinks";
    wantedBy = [ "multi-user.target" ];
    after = [ "avahi-daemon.service" "network-online.target" ];
    requires = [ "avahi-daemon.service" ];
    wants = [ "network-online.target" ];
    script = ''
      published=0
      while read -r iface addr; do
        [ -z "$addr" ] && continue
        ${config.services.avahi.package}/bin/avahi-publish --address --no-reverse \
          ${lib.escapeShellArg alias} "$addr" >> /dev/null 2>&1 &
        published=$((published + 1))
      done < <(
        ${pkgs.iproute2}/bin/ip -4 -o addr show up \
          | ${pkgs.gawk}/bin/awk '
            ! / lo / && $2 !~ /^(tun|tap|wg|br|veth|docker|virbr)/ {
              if (match($4, /^([0-9.]+)\/[0-9]+/, m)) print $2, m[1]
            }'
      )
      if [ "$published" -eq 0 ]; then
        echo "no usable IPv4 uplink to publish ${alias} on" >&2
        exit 1
      fi
      wait
    '';
    serviceConfig = {
      Restart = "always";
      RestartSec = 5;
    };
  };
in
{
  imports = [ ../tcp-gateway/config.nix ];

  config = {
    services.caddy.virtualHosts = {
      "http://${statusHost}".extraConfig = statusSiteConfig;
      # f13-01: static SPA acp-web на LAN-имени acp-ui.<node>.local.
      # f14: acp-ui может быть защищён Authentik ForwardAuth (см. wrapForwardAuth).
      "http://${acpUiHost}".extraConfig = wrapForwardAuth "acp-ui" acpUiSiteConfig;
    } // lib.optionalAttrs (statusMeshHost != null) {
      "${statusMeshHost}".extraConfig = statusSiteConfig;
    } // lib.optionalAttrs (acpUiMeshHost != null) {
      # f13-01 (mesh): внешний HTTPS-доступ к acp-ui на mesh-домене — тот же
      # статический SPA-контент, что и на LAN.
      "${acpUiMeshHost}".extraConfig = wrapForwardAuth "acp-ui" acpUiSiteConfig;
    };

    systemd.services.lattice-node-status = {
      description = "Lattice node status API and system metrics exporter";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      environment = {
        NODE_STATUS_LISTEN_ADDRESS = "${statusAddress}:${toString statusPort}";
        NODE_STATUS_STATE_VERSION = config.system.stateVersion;
        NODE_STATUS_COMIN_REPO = cominSourceRepo;
        NODE_STATUS_SYSTEMCTL = "${pkgs.systemd}/bin/systemctl";
        NODE_STATUS_SYSTEMD_UNITS = lib.concatStringsSep " " [
          "caddy.service"
          "comin.service"
          "llm-gateway.service"
          "prometheus.service"
          "loki.service"
          "alloy.service"
          "grafana.service"
          "rns-server.service"
          "rnsh.service"
        ];
      };
      serviceConfig = {
        ExecStart = lib.getExe pkgs.lattice.node-status;
        Restart = "on-failure";
        RestartSec = 5;
        DynamicUser = true;
        NoNewPrivileges = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        CapabilityBoundingSet = "";
        SystemCallArchitectures = "native";
      };
    };

    systemd.services.node-status-mdns = mdnsPublishService statusHost;
    # f13-01: alias для web-клиента ACP. Отдельный юнит: статус-сайт и acp-ui
    # живут независимо, и падение одного alias не роняет другой.
    systemd.services.acp-ui-mdns = mdnsPublishService acpUiHost;
  };
}
