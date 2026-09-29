{ nixpkgs, pkgs, appServicesProfile }:

let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      appServicesProfile
      {
        # Use the overlaid pkgs (which carries pkgs.lattice.*, incl. acp-web),
        # not bare legacyPackages, so the profile's acp-web site resolves.
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";
      }
    ];
  }).config;

  statusHost = "status.node-a.local";
  acpUiHost = "acp-ui.node-a.local";
  gateway = config.services.caddy.virtualHosts."http://${statusHost}";
  acpUi = config.services.caddy.virtualHosts."http://${acpUiHost}";
  statusService = config.systemd.services.lattice-node-status;

  # f13-01 (mesh, 2026-09-20): как и status, acp-ui живёт не только на LAN;
  # на mesh-домене тот же статический SPA обслуживается по HTTPS (f4-05
  # acme_dns) — с тем же extraConfig, что и LAN-сайт. Это контракт: вне пары
  # LAN+mesh сайт должен быть один и тот же (не копия конфига).
  meshConfig = (lib.nixosSystem {
    modules = [
      appServicesProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";
        lattice.tcp-gateway.meshDomain = "homelab.myt.su";
        lattice.tcp-gateway.cloudflareToken = "/run/agenix/caddy-cloudflare-token";
      }
    ];
  }).config;
  meshUi = meshConfig.services.caddy.virtualHosts."https://acp-ui.homelab.myt.su";
  meshUiLan = meshConfig.services.caddy.virtualHosts."http://acp-ui.node-a.local";
  meshStatus = meshConfig.services.caddy.virtualHosts."https://status.homelab.myt.su";
in
assert !config.services.nginx.enable;
assert config.services.caddy.enable;
# Status ingress proxies the loopback-only Go backend; its private port is not
# opened in the firewall and the process runs under a strict sandbox.
assert lib.hasInfix "reverse_proxy 127.0.0.1:9217" gateway.extraConfig;
assert lib.hasInfix "respond @metrics 404" gateway.extraConfig;
assert lib.hasInfix "node-status" statusService.serviceConfig.ExecStart;
assert statusService.environment.NODE_STATUS_STATE_VERSION == "26.05";
assert statusService.environment.NODE_STATUS_COMIN_REPO == "/var/lib/comin/source/repository";
assert lib.hasInfix "caddy.service" statusService.environment.NODE_STATUS_SYSTEMD_UNITS;
assert statusService.serviceConfig.DynamicUser;
assert statusService.serviceConfig.NoNewPrivileges;
assert statusService.serviceConfig.ProtectSystem == "strict";
assert !(builtins.elem 9217 config.networking.firewall.allowedTCPPorts);
assert config.services.avahi.publish.userServices;
# f13-01: acp-ui LAN site отдаёт статический SPA из store-path пакета acp-web;
# SPA-fallback на index.html (клиентская маршрутизация).
assert builtins.hasAttr "acp-ui-mdns" config.systemd.services;
assert lib.hasInfix acpUiHost config.systemd.services."acp-ui-mdns".script;
assert lib.hasInfix "file_server" acpUi.extraConfig;
assert lib.hasInfix "try_files {path} /index.html" acpUi.extraConfig;
assert lib.hasInfix "acp-web" acpUi.extraConfig;
# mDNS alias публикуется и для статуса, и для acp-ui.
assert builtins.hasAttr "node-status-mdns" config.systemd.services;
assert builtins.hasAttr "acp-ui-mdns" config.systemd.services;
assert lib.hasInfix statusHost config.systemd.services.node-status-mdns.script;
assert lib.hasInfix acpUiHost config.systemd.services."acp-ui-mdns".script;
# Публикация идёт по ВСЕМ IPv4-аплинкам, а не по адресу из default-route
# (`route get … src`) — на многодомной ноде это молча оставляет алиас пустым в
# подсетях, чей интерфейс не держит маршрут по умолчанию (инцидент с auth-mdns
# на 192.168.3.12 вместо 192.168.60.184 после F14 restart).
assert !(lib.hasInfix "route get 1.1.1.1" config.systemd.services.node-status-mdns.script);
assert !(lib.hasInfix "route get 1.1.1.1" config.systemd.services."acp-ui-mdns".script);
assert lib.hasInfix "addr show up" config.systemd.services.node-status-mdns.script;
assert lib.hasInfix "addr show up" config.systemd.services."acp-ui-mdns".script;
# avahi-publish должен запускаться в фоне (2>&1 &): иначе долгоживущий процесс
# блокирует while-цикл на первом учебнике и остальные аплинки не публикуются
# (инцидент: все алиасы уехали на 192.168.3.12 после F14 deploy).
assert lib.hasInfix "2>&1 &" config.systemd.services.node-status-mdns.script;
assert lib.hasInfix "2>&1 &" config.systemd.services."acp-ui-mdns".script;
# HTTP-status endpoint must be reachable; extra ports may legitimately be added.
assert builtins.elem 80 config.networking.firewall.allowedTCPPorts;
# f13-01 (mesh): acp-ui имеет mesh-HTTPS site на той же статике, что и LAN;
# без него регрессирует «SSL не работает» на https://acp-ui.homelab.myt.su.
assert builtins.hasAttr "https://acp-ui.homelab.myt.su" meshConfig.services.caddy.virtualHosts;
assert meshUi.extraConfig == meshUiLan.extraConfig;
# f14 fix (mesh 404): the mesh site's extraConfig must NOT pin/rewrite
# X-Forwarded-Host/Host to the LAN host. Authentik's embedded outpost matches the
# app strictly by X-Forwarded-Host/Host against the provider's external_host (one
# provider per host), so a per-host mesh provider
# (the generated Authentik Blueprint) can only match if the mesh site sends
# its own host. A rewrite to the LAN host would never match the mesh provider and
# would 404 the same way. (This pure app-services test has SSO off — forward_auth
# is absent here; the forward_auth-on-mesh contract is covered in tests/authentik.nix.)
assert !(lib.hasInfix "X-Forwarded-Host" meshUi.extraConfig);
assert !(lib.hasInfix "header_up Host" meshUi.extraConfig);
assert builtins.hasAttr "http://acp-ui.node-a.local" meshConfig.services.caddy.virtualHosts;
# status тоже остаётся на mesh (не регрессия соседнего сайта).
assert builtins.hasAttr "https://status.homelab.myt.su" meshConfig.services.caddy.virtualHosts;
assert meshStatus.extraConfig == meshConfig.services.caddy.virtualHosts."http://status.node-a.local".extraConfig;
pkgs.runCommand "app-services-profile-evaluation" { } "touch $out"
