{ nixpkgs, pkgs, gitCacheModule, gitCacheProfile, gatewayProfile }:

# f9-01/f9-02: модульный контракт Git cache proxy и валидация сгенерированного
# Caddy-конфига (caddy adapt --validate). Проверяются семантически важные
# свойства сервиса, а не точное равенство: песочница systemd, loopback-only
# публикация через ингресс, отсутствие upstream-credential до per-repo auth
# (f9-02), ReadWritePaths ограничены cache root, а repo-scoped allowlist
# (allowRepos) попадает в ExecStart как repeatable `--allow-repo`.
let
  inherit (nixpkgs) lib;
  config = (lib.nixosSystem {
    modules = [
      gitCacheModule
      gitCacheProfile
      gatewayProfile
      {
        nixpkgs.pkgs = pkgs;
        networking.hostName = "node-a";
        system.stateVersion = "26.05";
        lattice.git-cache-proxy.allowRepos = [ "mytecor/lattice" ];
      }
    ];
  }).config;

  cfg = config.lattice.git-cache-proxy;
  unit = config.systemd.services.git-cache-proxy;
  site = config.services.caddy.virtualHosts."http://git-cache-proxy.node-a.local";
  execStart = unit.serviceConfig.ExecStart;

  # Модульный assertion (f9-02): upstream credential без непустого allowRepos
  # падает. Проверяем на отдельном изолированном конфиге, как в rns-tcp.nix
  # (invalid input -> assertion fails).
  credentialWithoutAllowlist =
    (lib.nixosSystem {
      modules = [
        gitCacheModule
        {
          nixpkgs.pkgs = pkgs;
          system.stateVersion = "26.05";
          lattice.git-cache-proxy = {
            enable = true;
            host = "127.0.0.1";
            port = 19211;
            upstream = "https://github.com";
            upstreamAuthHeaderFile = "/run/credentials/upstream-auth";
            allowRepos = [ ];
          };
        }
      ];
    }).config.assertions;
in
assert cfg.enable;
assert cfg.host == "127.0.0.1";
assert cfg.port == 9211;
assert cfg.upstream == "https://github.com";
assert cfg.cacheRoot == "/var/cache/git-cache-proxy";
assert cfg.allowRepos == [ "mytecor/lattice" ];

# Песочница: строгий systemd-изолёт, никаких capabilities, read-write только
# cache root. Модуль звучит как «механизм» — верен для любого пользователя.
assert unit.serviceConfig.NoNewPrivileges == true;
assert unit.serviceConfig.ProtectSystem == "strict";
assert unit.serviceConfig.CapabilityBoundingSet == "";
assert unit.serviceConfig.MemoryDenyWriteExecute == true;
assert unit.serviceConfig.RestrictRealtime == true;
assert unit.serviceConfig.RestrictNamespaces == true;
assert builtins.elem "/var/cache/git-cache-proxy" unit.serviceConfig.ReadWritePaths;
assert unit.serviceConfig.User == "git-cache-proxy";
assert unit.serviceConfig.Group == "git-cache-proxy";

# tmpfiles создаёт cache root с владельцем сервисного юзера.
assert builtins.elem "d /var/cache/git-cache-proxy 0700 git-cache-proxy git-cache-proxy - -"
  config.systemd.tmpfiles.rules;

# Repo-scoped allowlist (f9-02) присутствует в сгенерированном wrapper-скрипте
# (ExecStart ведёт на него; флаг `--allow-repo` живёт внутри скрипта, не в
# строке пути). Upstream-credential в этом пролфиле не задан, поэтому
# LoadCredential пуст.
assert unit.serviceConfig.LoadCredential == [ ];

# assert: upstream credential требует непустой allowlist (f9-02).
assert config.lattice.git-cache-proxy.upstreamAuthHeaderFile == null;
assert config.lattice.git-cache-proxy.serveTokenFile == null;

# Caddy публикует прокси на mDNS alias и ходит в loopback-бэкенд.
assert site.extraConfig == "reverse_proxy 127.0.0.1:9211\n";
assert builtins.hasAttr "git-cache-proxy-mdns" config.systemd.services;

# ExecStart ведёт на исполняемый wrapper-скрипт модуля.
assert lib.hasPrefix "/nix/store/" (toString execStart);
assert lib.hasSuffix "git-cache-proxy-exec" (toString execStart);

# Модульный assertion: credential без allowlist падает (см. let-блок выше).
assert lib.any (a: !a.assertion) credentialWithoutAllowlist;

pkgs.runCommand "git-cache-proxy-config-check" {
  nativeBuildInputs = [ pkgs.caddy ];
} ''
  mkdir -p "$out"

  # f9-02: сгенерированный wrapper содержит repeatable --allow-repo для каждого
  # репозитория из allowRepos.
  grep -qF "--allow-repo=mytecor/lattice" ${execStart} \
    || { echo "git-cache-proxy: allowRepos not in exec wrapper" >&2; exit 1; }

  export XDG_DATA_HOME="$TMPDIR/caddy-data"
  export XDG_CONFIG_HOME="$TMPDIR/caddy-config"
  caddy adapt \
    --config ${config.services.caddy.configFile} \
    --adapter caddyfile \
    --validate > "$out/caddy-config.json"

  touch "$out/ok"
''
