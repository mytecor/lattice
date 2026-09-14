{ nixpkgs, pkgs, verdaccioModule, verdaccioProfile, cachePlaneModules }:

# f9-03: модульный контракт Verdaccio (cache-only npm/pnpm/yarn proxy).
# Проверяются семантически важные свойства сервиса, а не точное равенство:
# loopback-only привязка, cache-only publish (из вне-store htpasswd),
# ReadWritePaths ограничены cache root, строгая systemd-песочница —
# за исключением MemoryDenyWriteExecute, который обязан быть false, потому что
# Node 24/V8 не может инициализировать isolate под W^X-политикой systemd
# (v8::base::OS::SetPermissions возвращает EPERM вместо ожидаемого ENOMEM ->
# "Check failed: 12 == (*__errno_location ())"). Этот контракт ловит регрессию,
# когда кто-то снова включит MDWX и сервис перестанет стартовать на реальной
# ноде (mytecor-homelab, nodejs-24.19.0), ломая comin-переключения.
let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = cachePlaneModules ++ [
      verdaccioModule
      verdaccioProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";
      }
    ];
  }).config;

  cfg = config.lattice.verdaccio;
  unit = config.systemd.services.verdaccio;
in
assert cfg.enable;
assert cfg.host == "127.0.0.1";
assert cfg.port == 9212;
assert cfg.cacheRoot == "/var/cache/verdaccio";
assert cfg.upstreamRegistry == "https://registry.npmjs.org";

# Cache-only proxy: publish выключен по умолчанию, credentials не требуются.
assert cfg.publish == false;
assert cfg.credentials.htpasswdFile == null;

# Песочница: строгий изолёт, никаких capabilities, read-write только cache root.
# MemoryDenyWriteExecute обязан оставаться false — это не ослабление, а
# совместимость с Node 24/V8 (см. README модуля); остальные защитные опции
# остаются включёнными.
assert unit.serviceConfig.NoNewPrivileges == true;
assert unit.serviceConfig.ProtectSystem == "strict";
assert unit.serviceConfig.CapabilityBoundingSet == "";
assert unit.serviceConfig.MemoryDenyWriteExecute == false;
assert unit.serviceConfig.RestrictRealtime == true;
assert unit.serviceConfig.RestrictNamespaces == true;
assert unit.serviceConfig.LockPersonality == true;
assert builtins.elem "AF_UNIX" unit.serviceConfig.RestrictAddressFamilies;
assert builtins.elem "AF_INET" unit.serviceConfig.RestrictAddressFamilies;
assert builtins.elem "AF_INET6" unit.serviceConfig.RestrictAddressFamilies;
assert builtins.elem "/var/cache/verdaccio" unit.serviceConfig.ReadWritePaths;
assert unit.serviceConfig.User == "verdaccio";
assert unit.serviceConfig.Group == "verdaccio";
assert unit.serviceConfig.WorkingDirectory == "/var/cache/verdaccio";

# Disposable-cache contract: CacheDirectory создаёт cacheRoot до mount
# namespacing на каждый старт (tmpfiles — только на boot, поэтому после
# `rm -rf /var/cache/verdaccio` во время работы сервис падал с 226/NAMESPACE).
# Live-проверка f9-03 на mytecor-homelab это показала (2026-09-14).
assert unit.serviceConfig.CacheDirectory == "verdaccio";
assert unit.serviceConfig.CacheDirectoryMode == "0700";
assert unit.serviceConfig.WorkingDirectory == "/var/cache/verdaccio";

# Cache-only: никакой htpasswd из LoadCredential (publish выключен).
assert unit.serviceConfig.LoadCredential == [ ];

# clientConfig пишет только URL реестра, никаких credentials — и только в
# места, которые реальный package manager ноды (pnpm) читает (проверено на
# ноде, 2026-09-14): /root/.config/pnpm/config.yaml реестра pnpm 11, +
# /etc/npmrc для не-Nix npm.
assert builtins.hasAttr "npmrc" config.environment.etc;
assert config.environment.etc.npmrc.text == "registry=http://127.0.0.1:9212/\n";
# npmrc не содержит ничего похожего на credential.
assert !lib.strings.hasInfix "token" config.environment.etc.npmrc.text;
assert !lib.strings.hasInfix "password" config.environment.etc.npmrc.text;

# Activation script пересоздаёт pnpm config.yaml (путь вне /etc, /root
# ephemeral по impermanence) при каждой активации.
let
  actScript = config.system.activationScripts.verdaccioClientConfig.text;
in
assert lib.strings.hasInfix "registry: http://127.0.0.1:9212/" actScript;
assert lib.strings.hasInfix "/root/.config/pnpm/config.yaml" actScript;
# В активационном скрипте — только URL реестра, никаких credentials.
assert !lib.strings.hasInfix "token" actScript;
assert !lib.strings.hasInfix "password" actScript;

# Генерируемый verdaccio config.yaml обязан содержать literal ACL-токен
# `access: $anonymous` (одиночный $, без фигурных скобок). Ранее здесь стоял
# Nix-экейп `\${anonymous}`, который попадал в YAML как literal `${anonymous}`:
# @verdaccio/config ROLES знает только $anonymous/$all/$authenticated (и
# @-deprecated), поэтому аннонимный клиент получал 401 "authorization\n# required" на каждую раздачу и cold install был невозможен. Live-проверка
# f9-03 на mytecor-homelab это поймала (2026-09-14).
let
  yaml = config.lattice.verdaccio.generatedConfigYaml;
  dollar = "$";
in
assert lib.strings.hasInfix "access: $anonymous" yaml;
# ${...} форма токена (Nix-экейп `\${...}`) никогда не должна вернуться.
assert !lib.strings.hasInfix "${dollar}{anonymous}" yaml;
# cache-only: publish/unpublish отсутствуют (default ACL = deny всем).
assert !lib.strings.hasInfix "publish:" yaml;
assert !lib.strings.hasInfix "unpublish:" yaml;

# В publish-режиме ACL переключается на $authenticated и присутствует htpasswd.
let
  publishConfig = (lib.nixosSystem {
    modules = cachePlaneModules ++ [
      verdaccioModule
      verdaccioProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";
        lattice.verdaccio.publish = true;
        lattice.verdaccio.credentials.htpasswdFile = "/tmp/htpasswd";
      }
    ];
  }).config;
  yamlP = publishConfig.lattice.verdaccio.generatedConfigYaml;
in
assert lib.strings.hasInfix "publish: $authenticated" yamlP;
assert lib.strings.hasInfix "unpublish: $authenticated" yamlP;
assert builtins.elem "htpasswd:/tmp/htpasswd" publishConfig.systemd.services.verdaccio.serviceConfig.LoadCredential;

pkgs.runCommand "verdaccio-config-check" { } "touch $out"
