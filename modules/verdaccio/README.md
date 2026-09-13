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

## Границы безопасности

1. Сервис слушает только **loopback**; наружу публикуется только через
   ингресс `tcp-gateway` (Caddy), если это операторски оправдано. Для local
   tooling ноды внешняя публикация не нужна.
2. По умолчанию registry — **read-only cache**: `access: $anonymous`
   (anonymous read в пределах закрытой сети), `publish/unpublish` — `$none`
   (никто). Публикация включается только явной опцией `publish = true` вместе
   с htpasswd-файлом вне Nix store. В генерируемый store-конфиг секреты не
   попадают; htpasswd монтируется через `LoadCredential`.
3. Строгий песочник systemd: `NoNewPrivileges`,
   `ProtectSystem=strict` (`ReadWritePaths` — только cache root),
   `MemoryDenyWriteExecute`, `RestrictAddressFamilies`
   (`AF_UNIX`/`AF_INET`/`AF_INET6`), ограниченные system calls.
4. Кешированные данные — disposable: удаление `cacheRoot` вызывает обычный
   refetch из uplink при следующем cold install; cache никогда не является
   source of truth.

## Кеш — не source of truth

Verdaccio кеширует только то, что запросили клиенты; удаление cache не теряет
source, пакеты восстанавливаются из upstream registry и lockfiles.
