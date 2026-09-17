# Verdaccio npm caching proxy

Модуль предоставляет `lattice.verdaccio.enable` — systemd-сервис
[Verdaccio](https://verdaccio.org) как **cache-only npm/pnpm/yarn proxy
(f9-03)**. Сервис слушает только loopback, читает anonymous из uplink и
раздаёт клиентам ноды закешированные метаданные и tarball'ы.

## Опции

- `lattice.verdaccio.enable` — включить сервис.
- `lattice.verdaccio.package` — пакет (по умолчанию
  `pkgs.lattice.verdaccio`, собранный через общий
  [pnpm CLI builder](../../packages/pnpm-cli-builder/README.md)).
- `lattice.verdaccio.user` / `group` — system user/group
  (по умолчанию `verdaccio`).
- `lattice.verdaccio.host` / `port` — адрес привязки (по умолчанию
  `127.0.0.1:9212`); наружу не публикуется.
- `lattice.verdaccio.upstreamRegistry` — upstream npm registry
  (по умолчанию `https://registry.npmjs.org`).
- `lattice.verdaccio.cacheRoot` — каталог кеша (по умолчанию
  `/var/cache/verdaccio`, persistent, disposable).
- `lattice.verdaccio.publish` — разрешить publish/unpublish (по умолчанию
  `false`: cache-only). При `true` требуется
  `credentials.htpasswdFile` (htpasswd через `LoadCredential`), publish
  требует авторизованного пользователя.
- `lattice.verdaccio.credentials.htpasswdFile` — runtime path к htpasswd
  (например agenix `.age`), монтируется через systemd `LoadCredential`,
  в argv не попадает.
- `lattice.verdaccio.maxBodySize` — лимит тела запроса (по умолчанию `10mb`).
- `lattice.verdaccio.logLevel` — уровень лога stdout
  (`fatal`..`trace`, по умолчанию `warn`).
- `lattice.verdaccio.runtimeDirectory` — systemd RuntimeDirectory.
- `lattice.verdaccio.clientConfig` (default true) — декларативный registry-конфиг
  pnpm ноды на loopback-прокси (см. ниже).

## Границы безопасности

1. Сервис слушает только **loopback**; наружу публикуется только через
   ингресс `tcp-gateway` (Caddy), если это операторски оправдано. Для local
   tooling ноды внешняя публикация не нужна.
2. По умолчанию registry — **read-only cache**: `access: $anonymous`
   (anonymous read в пределах закрытой сети); `publish/unpublish` не
   объявлены (verdaccio по умолчанию даёт пустой ACL, то есть никто).
   Публикация включается только явной опцией `publish = true` вместе
   с htpasswd-файлом вне Nix store; тогда ACL переключается на
   `$authenticated`. В генерируемый store-конфиг секреты не попадают;
   htpasswd монтируется через `LoadCredential`.
3. Строгий песочник systemd: `NoNewPrivileges`,
   `ProtectSystem=strict` (`ReadWritePaths` — только cache root),
   `RestrictAddressFamilies`
   (`AF_UNIX`/`AF_INET`/`AF_INET6`), ограниченные system calls.
   `MemoryDenyWriteExecute` **отключён явно**: Node 24/V8 не может создать
   isolate при W^X-политике systemd — `v8::base::OS::SetPermissions` на
   code range падает с `EPERM` и V8 аварийно завершается на
   `Check failed: 12 == (*__errno_location ())` ещё до старта verdaccio
   (воспроизведено на `mytecor-homelab`, nodejs-24.19.0). Это тот же трейд-офф,
   что у [llm-gateway](../../modules/llm-gateway/README.md), где
   `MemoryDenyWriteExecute = false` из-за `mprotect(PROT_EXEC)` зависимостей:
   остальная жёсткость песочника (NoNewPrivileges, ProtectSystem=strict,
   CapabilityBoundingSet="", syscall filter) сохраняется.
4. Кешированные данные — disposable: удаление `cacheRoot` вызывает обычный
   refetch из uplink при следующем cold install; cache никогда не является
   source of truth.

## Кеш — не source of truth

Verdaccio кеширует только то, что запросили клиенты; удаление cache не теряет
source, пакеты восстанавливаются из upstream registry и lockfiles.

## Client config (pnpm)

`clientConfig` направляет pnpm ноды на loopback-прокси без ручной настройки.
Единственный реально используемый пакетный менеджер проекта/ноды — pnpm
(все Node-пакеты собираются через `buildPnpmCli`); yarn не поддерживается.
Расположение конфига (проверено на `mytecor-homelab`, 2026-09-14):

- **pnpm 11** `/etc/npmrc` НЕ читает (globalconfig —
  `$XDG_CONFIG_HOME/pnpm/config.yaml`); `/etc/pnpmrc` и env `NPM_CONFIG_REGISTRY`
  игнорируются. Файл пересоздаётся активацией в `/root/.config/pnpm/config.yaml`
  (путь вне `/etc`, `/root` ephemeral по impermanence).
- `/etc/npmrc` пишется для не-Nix npm в shell-сессиях, где он применим.

Пишется только URL реестра (`http://${host}:${port}/`), никаких credentials; для
других пользователей/хостов прокси закрыт loopback-привязкой.
