{ config, lib, pkgs, ... }:

let
  hostName = config.networking.hostName;
  statusHost = "status.${hostName}.local";

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

  # Общий extraConfig статус-сайта (LAN и mesh используют один и тот же контент-блок).
  statusSiteConfig = ''
    # f4-04: единый статус-файл отдаётся на любой путь. root указывает на
    # каталог /run, а rewrite перенаправляет запрос на сам файл, чтобы
    # file_server не делал 308-редирект (трактуя root-файл как директорию).
    root * /run
    rewrite * /lattice-node-status.json
    header Content-Type application/json
    file_server
  '';

  # f4-04: JSON генерируется на каждой активации из runtime-фактов узла
  # (NixOS generation, применённый comin commit). Обслуживает сам Caddy через
  # file_server — без отдельного backend-процесса и внутреннего порта
  # (контракт f4-02 сохранён).
  statusFile = "/run/lattice-node-status.json";
  cominSourceRepo = "/var/lib/comin/source/repository";
  # f4-04: writer статус-документа. Собираем через replaceVarsWith: вшиваем
  # полные store-пути bash/git/jq (@bash@/@git@/@jq@) — их нет в активационной
  # среде NixOS (иначе activation падал 127 и валил comin-switch). Остальные
  # команды (readlink/basename/hostname/uname/date/chmod/mv) уже есть в PATH
  # активации через coreutils. dir="bin" + isExecutable дают executable в
  # $out/bin/lattice-node-status-write.
  statusWriterPkg = pkgs.replaceVarsWith {
    name = "lattice-node-status-write";
    src = ./status-write.sh;
    replacements = {
      bash = "${pkgs.bash}/bin/bash";
      git = "${pkgs.git}/bin/git";
      jq = "${pkgs.jq}/bin/jq";
      hostname = "${pkgs.inetutils}/bin/hostname";
    };
    dir = "bin";
    isExecutable = true;
  };
  statusWriter = "${statusWriterPkg}/bin/lattice-node-status-write";

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

  # f13-01: публикация service-specific mDNS alias (avahi-publish --address)
  # — тот же приём, что у статус-сайта; параметризуем, чтобы не дублировать
  # юнит. Алias отдельного имения убирает необходимость в Host-заголовке и
  # дополнительной DNS-записи на стороне клиента.
  mdnsPublishService = alias: {
    description = "Publish the ${alias} mDNS alias";
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
      exec ${config.services.avahi.package}/bin/avahi-publish --address --no-reverse \
        ${lib.escapeShellArg alias} "$address"
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
      "http://${acpUiHost}".extraConfig = acpUiSiteConfig;
    } // lib.optionalAttrs (statusMeshHost != null) {
      "${statusMeshHost}".extraConfig = statusSiteConfig;
    } // lib.optionalAttrs (acpUiMeshHost != null) {
      # f13-01 (mesh): внешний HTTPS-доступ к acp-ui на mesh-домене — тот же
      # статический SPA-контент, что и на LAN.
      "${acpUiMeshHost}".extraConfig = acpUiSiteConfig;
    };

    # f4-04: пишем статус-документ при каждой активации. Специальный
    # 'lattice-node-status' activation script работает без отдельного юнита:
    # значения generation/commit меняются именно на активации, а Caddy читает
    # файл только по запросу. Если коммит ещё не выбран (свежая нода до первого
    # comin-цикла), commit=null, endpoint остаётся валидным JSON.
    system.activationScripts.lattice-node-status = {
      deps = [ ];
      text = ''
        LATTICE_NODE_STATUS_FILE=${statusFile} \
        LATTICE_NODE_STATE_VERSION=${lib.escapeShellArg config.system.stateVersion} \
        LATTICE_COMIN_SOURCE_REPO=${lib.escapeShellArg cominSourceRepo} \
        ${statusWriter}
      '';
    };

    systemd.services.node-status-mdns = mdnsPublishService statusHost;
    # f13-01: alias для web-клиента ACP. Отдельный юнит: статус-сайт и acp-ui
    # живут независимо, и падение одного alias не роняет другой.
    systemd.services.acp-ui-mdns = mdnsPublishService acpUiHost;
  };
}
