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

# tmpfiles создаёт cache root с владельцем сервисного юзера.
assert builtins.elem "d /var/cache/verdaccio 0700 verdaccio verdaccio - -"
  config.systemd.tmpfiles.rules;

# Cache-only: никакой htpasswd из LoadCredential (publish выключен).
assert unit.serviceConfig.LoadCredential == [ ];

# clientConfig пишет только URL реестра, никаких credentials.
assert builtins.hasAttr "npmrc" config.environment.etc;
assert builtins.hasAttr "yarnrc" config.environment.etc;
assert config.environment.etc.npmrc.text == "registry=http://127.0.0.1:9212/\n";
assert config.environment.etc.yarnrc.text == "registry \"http://127.0.0.1:9212/\"\n";
# npmrc/yarnrc не содержат ничего похожего на credential.
assert !lib.strings.hasInfix "token" config.environment.etc.npmrc.text;
assert !lib.strings.hasInfix "password" config.environment.etc.yarnrc.text;

pkgs.runCommand "verdaccio-config-check" { } "touch $out"
