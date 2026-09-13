# Attic module

Модуль предоставляет `lattice.attic.enable` — systemd-сервис
[`atticd`](https://github.com/zhaofengli/attic) — Nix binary cache / artifact
cache для Lattice cache-plane (F9). Это **disposable** accelerator: потеря
данных cache вызывает обычный rebuild/refetch, но не ломает воспроизводимость
(подпись nar определяет доверие, а не расположение worker).

## Выбор runtime (закрытие открытого вопроса f9-04)

**Выбран `attic-server` из nixpkgs** (даемон `atticd`, пакет `attic`);
клиент — `attic-client` из nixpkgs (CLI `attic`). Никакого форка и локального
пакета не нужно. Оба пакета linux-only Rust; на macOS они **evaluate**-ятся, но
не собираются и не запускаются — сборку и исполняемую проверку выполняет CI
(ubuntu x86_64-linux).

Проверено по pinned revision `4533d9293756b63904b7238acb84ac8fe4c8c2c4`
(`attic-server 0-unstable-2026-07-06`, `attic-client 0-unstable-2026-07-06`):

- `attic-server` предоставляет бинарь `atticd` и конфиг в TOML; JWT-secret для
  admin-токенов читается из env (`ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64` /
  `ATTIC_SERVER_TOKEN_RS256_SECRET_BASE64`) либо из `[jwt.signing]` TOML.
- Подпись nar-файлов: **signing keypair генерируется и хранится server-side в
  БД** (`KeypairConfig::Generate` при `attic cache create`), публичный ключ
  отдаётся через `attic cache info`. Отдельного бинаря `attic keygen`/`atticd
  keygen` в закреплённой версии нет; ротация пары — `attic cache configure
  --regenerate-keypair` (см. README ниже).
- Клиентские команды (`login`, `cache create/configure/info`, `push`, `use`,
  `watch-store`) — из `attic-client`. `attic use` сам прописывает
  `~/.config/nix/nix.conf`; в нашем модуле эквивалентная client-обвязка
  декларативна через `nix.settings`.

## Опции

- `lattice.attic.enable` — включить сервис.
- `lattice.attic.package` — пакет даемона (по умолчанию `pkgs.attic-server`).
- `lattice.attic.clientPackage` — пакет CLI `attic` (по умолчанию
  `pkgs.attic-client`), кладётся в `environment.systemPackages`.
- `lattice.attic.user` / `group` — system user/group (по умолчанию `attic`).
- `lattice.attic.host` / `port` — адрес привязки (по умолчанию loopback);
  публикацию наружу делает оператор (Caddy `tcp-gateway` ingress), а не
  открытый firewall-порт.
- `lattice.attic.dataRoot` — постоянный корень SQLite БД и локального NAR
  storage (по умолчанию `/var/lib/attic`, disposable).
- `lattice.attic.runtimeDirectory` — приватный systemd `/run` каталог.
- `lattice.attic.cacheName` — имя кеша (binary cache этой ноды).
- `lattice.attic.tokenSecretFile` — env-файл (`KEY="value"`) с JWT-secret для
  admin-токенов (`ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64=…` либо RS256);
  runtime path (agenix secret), монтируется через systemd `LoadCredential`, в
  argv и Nix store не попадает. `null` = без JWT (pull/push подписанных nars
  работает, администрирование кеша — через отдельно развёрнутый токен).
- `lattice.attic.trustedPublicKey` — публичный signing key кеша в форме
  `<keyName>:<base64>` (из `attic cache info`). Публичная половина пары,
  приватная никогда не покидает сервер. `null` = placeholder (client-обвязка
  не эмитируется).
- `lattice.attic.publicUrl` — канонический URL кеша для клиентов (без имени
  кеша, с завершающим слэшем). `null` = loopback URL (`http://<host>:<port>`),
  годится только для локального fetch ноды.
- `lattice.attic.allowUnauthenticatedRead` — создавать кеш `--public` (pull без
  токена). Push всегда требует токен.
- `lattice.attic.substituter` — регистрировать локальный attic в
  `nix.settings.substituters` + `trusted-public-keys` этой ноды (принимаются
  ТОЛЬКО nars, подписанные ключом кеша). Эмитируется только когда заданы и
  `trustedPublicKey`, и `publicUrl`; иначе — no-op placeholder.

## Модель доверия / подписи

- Кеш подписывает nars **серверный signing keypair** (Ed25519), который attic
  генерирует при `attic cache create` и хранит в своей БД. Приватная часть не
  является отдельным файлом и не передаётся в сервис — она рождается и живёт в
  БД на сервере.
- Клиенты (включая nix этой ноды) доверяют только **публичному** ключу кеша
  (`trustedPublicKey`). `nix.settings.trusted-public-keys` содержит только
  публичную половину; подпись проверяется nix локально.
- JWT-secret для admin-токенов (могут подписывать токены с правами на кеш) —
  отдельный секрет, попадает в сервис только через `LoadCredential`/env, в
  argv и Nix store не попадает. Это тот секрет, который реально конфигурирует
  atticd; signing keypair файлом не является.

## Sandbox systemd

Строгий песочный контур (по образцу `git-cache-proxy` и upstream `atticd.nix`):
`NoNewPrivileges`, `ProtectSystem=strict` + `ReadWritePaths=[dataRoot]`,
`CapabilityBoundingSet=""`, `MemoryDenyWriteExecute`, ограниченные syscalls и
address families, приватный `/run`, dedicated system user `attic`. Секрет —
через `LoadCredential`, не argv.

## Операции: GC, лимиты, ротация

### Garbage collection / лимиты

- Автоматический GC: в сгенерированном TOML `garbage-collection.interval =
  "12 hours"`. Ручной запуск с той же конфигурацией:
  `atticd -f server.toml --mode garbage-collector-once`.
- Retention на уровне кеша:
  `attic cache configure <cache> --retention-period '3 months'` (или `1d`, `1h`);
  сброс к глобальному дефолту — `--reset-retention-period`.
- GC трёхуровневый: local cache view → global NAR store → global chunk store
  (память освобождается только на уровне chunks).
- Лимит размера: у локального storage (и SQLite) жёсткой квоты из коробки нет;
  оператор контролирует размер диска `/var/lib/attic` (persistent storage) и
  retention-политикой. При необходимости ограничьте подписку storage на
  subvolume/том с лимитом в `nodes/<name>/disko.nix`.

### Ротация signing credentials (KEY_MANAGEMENT.md-конвенции)

Ротация **JWT admin-secret** (аналог rotation provider key):

1. На доверенной машине сгенерируйте новое значение:
   `openssl genrsa -traditional 4096 | base64 -w0`
   и положите его в файл вида `ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64="…"`.
2. Зашифруйте в `nodes/<name>/secrets/attic-jwt-secret.age` (recipients
   `[ admin node ]`), обновите содержимое `.age`.
3. Примените конфиг (сервис перезапускается), проверьте `attic login` новым
   токеном до отзыва старого JWT-секрета.

Ротация **signing keypair кеша**: `attic cache configure <cache>
--regenerate-keypair`, затем у всех клиентов обновить
`trustedPublicKey`/`trusted-public-keys` (публичная половина из `attic cache
info`). Это отдельная от JWT операция. При компрометации — изолировать ноду и
следовать [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md) «Отзыв
скомпрометированной ноды».

## Операторский шаг перед deploy (создание .age)

1. На **linux/builder** машине (на macOS attic не собирается) загрузите
   `attic-client` и `attic-server` в окружение, поднимите atticd и создайте
   кеш, либо воспользуйтесь уже развёрнутым сервисом ноды через `attic login`.
2. Создайте кеш и получите его публичный ключ:

   ```sh
   attic login local http://127.0.0.1:<port> <root-token>
   attic cache create <cacheName> --public
   attic cache info <cacheName>      # -> Public Key: <keyName>:<base64>
   ```

3. Сгенерируйте JWT-secret (admin) и положите в env-файл:
   `echo 'ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64="<base64>"' > attic-jwt-secret.env`.
4. Каждое значение зашифруйте в
   `nodes/mytecor-homelab/secrets/attic-jwt-secret.age` с
   `publicKeys [ admin node ]` через agenix и закоммитьте `.age` (см.
   «Подготовка плановой ротации» в [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md)).
   Никогда не коммитьте закрытые ключи/открытые значения.
5. Пропишите в node config `lattice.attic.trustedPublicKey = "<ключ из
   `attic cache info`>"` и `lattice.attic.publicUrl`, примените, проверьте:
   `nix-build .#nixosConfigurations.mytecor-homelab` и `nix copy`/fetch через
   substituter.

## Client substituter wiring

Модуль декларативно добавляет в `nix.settings` этой ноды:

```nix
{
  substituters = [ "<publicUrl>/<cacheName>" ];
  trusted-public-keys = [ "<trustedPublicKey>" ];
}
```

так что nix ноды fetch'ит подписанные nars через локальный attic. Пока
`trustedPublicKey`/`publicUrl` не заданы — никакая client-обвязка не
эмитируется (placeholder), оценка проходит без `.age` файла.
