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
  statusWriter = config.system.activationScripts.lattice-node-status;
in
assert !config.services.nginx.enable;
assert config.services.caddy.enable;
# f4-04: endpoint отдаёт runtime JSON из /run через file_server (не static respond),
# root на каталог /run + rewrite на файл (без 308-редиректа).
assert lib.hasInfix "file_server" gateway.extraConfig;
assert lib.hasInfix "lattice-node-status" gateway.extraConfig;
assert lib.hasInfix "/run" gateway.extraConfig;
assert lib.hasInfix "lattice-node-status.json" gateway.extraConfig;
assert !lib.hasInfix "respond" gateway.extraConfig;
# Активационный скрипт генерирует документ: stateVersion и commit source /var/lib/comin/source/repository.
assert lib.hasInfix "lattice-node-status-write" statusWriter.text;
assert lib.hasInfix "/var/lib/comin/source/repository" statusWriter.text;
assert lib.hasInfix "LATTICE_NODE_STATE_VERSION" statusWriter.text;
assert lib.hasInfix "26.05" statusWriter.text;
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
# HTTP-status endpoint must be reachable; extra ports may legitimately be added.
assert builtins.elem 80 config.networking.firewall.allowedTCPPorts;
pkgs.runCommand "app-services-profile-evaluation" { } "touch $out"
