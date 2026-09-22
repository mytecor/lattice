# lattice.authentik

NixOS-модуль центрального SSO (F14) поверх `pkgs.authentik` (2026.5.6) без Docker:
«стандартный» для NixOS способ — нативный модуль с systemd-юнитами, секреты только
runtime-файлами agenix, сервис слушает loopback и стоит за единственным внешним
Caddy-ингрессом (`profiles/tcp-gateway`).

## Что это

Authentik — единый вход для пользовательских web-сервисов ноды. В этой сборке nixpkgs
(`ak`-wrapper, подкоманды `server`/`worker`/`manage`/`healthcheck`) cache построен на
Postgres (`django_postgres_cache`), поэтому **Redis не требуется**. Модуль:

- включает `services.postgresql`, создаёт роль+БД `authentik` и пользуется дефолтным
  peer-auth по unix-сокету: OS-пользователь `authentik` == роль БД `authentik` →
  **пароль БД не нужен** и ни один секрет не попадает в Nix store;
- запускает `authentik-server` (loopback HTTP) и `authentik-worker` как непривилегированный
  пользователь `authentik` (`ak`-wrapper в не-root режиме пропускает docker/root-ветку);
- выполняет полный migration runner one-shot до старта сервера/воркера;
- генерирует единый нативный Authentik Blueprint для ForwardAuth и OIDC applications, включает
  proxy providers во встроенный outpost и применяет Blueprint до запуска Caddy;
- по умолчанию отключает все неиспользуемые listener'ы Authentik (HTTPS/LDAP/RADIUS/
  metrics/debug) и поднимает только HTTP на `127.0.0.1:<port>` — ничего не открывается наружу.

## Сервер и воркер: раздельные listener'ы (Rust-воркер)

`ak server` и `ak worker` — это **разные бинарники**: сервер это Python/Django (+ Go-proxy),
а воркер (2026.5.x) — Rust (`ak-common`, конфиг через config_rs/serde). Оба читают один
неймспейс `AUTHENTIK_LISTEN__*`, но по-разному:

- Django терпит пустое значение listener'а как «не биндить»;
- Rust-воркер парсит `listen.http`/`listen.metrics` как списки `SocketAddr` — **пустая строка
  становится `[""]` и падает** с `invalid socket address syntax` (crash-loop, который чинит
  этот сплит). Пустые `HTTPS/LDAP/LDAPS/RADIUS/DEBUG/DEBUG_PY` в Rust-воркере безвредны —
  таких полей в `ListenConfig` нет, serde их игнорирует;
- воркер **сам биндит** `listen.http` и `listen.metrics` (его healthcheck/metrics на TCP),
  поэтому не может делить `127.0.0.1:<port>` сервера.

Поэтому у воркера отдельный env: `AUTHENTIK_LISTEN__HTTP` и `AUTHENTIK_LISTEN__METRICS`
равны `127.0.0.1:0` (эфемерный порт, назначается ядром при bind — валиден для парсера и не
конфликтует ни с портом сервера, ни друг с другом). Наружу эти TCP-эндпоинты не нужны:
health-live/ready и metrics воркера ходят через unix-сокет, не по TCP.

## Безопасность / секреты

Все секреты — agenix-файлы по одному `AUTHENTIK_*=...` на файл, которые systemd грузит как
`EnvironmentFile` (в `serviceConfig.EnvironmentFile`). Значения не появляются ни в argv,
ни в store, ни в генерируемом конфиге. Обязательные (assertions):

- `secretKeyFile` — `AUTHENTIK_SECRET_KEY=...` (Django SECRET_KEY, без дефолта);
- `bootstrapTokenFile` — `AUTHENTIK_BOOTSTRAP_TOKEN=...` (постоянный API-токен оператора,
  `intent=api, expiring=false` — источник для декларативного провижининга);
- `bootstrapUserFile` / `bootstrapEmailFile` / `bootstrapPasswordFile` — создание
  оператора `akadmin`.

Bootstrap выполняется Authentik на первом старте из
[`system/bootstrap.yaml`](https://docs.goauthentik.io) blueprint'а: создаётся суперпользователь
и постоянный API-токен. Подробнее про automаted install —
[docs](https://docs.goauthentik.io/install-config/automated-install).

## Схема БД (peer-auth, без пароля)

Дефолтный nixpkgs `pg_hba`: `local all all peer` по unix-сокету. Сервис ходит
`AUTHENTIK_POSTGRESQL__HOST=/run/postgresql` как OS-пользователь `authentik` → peer-auth
успешен без пароля. `ensureDBOwnership` отдаёт БД `authentik` роли `authentik`. TCP loopback
оставлен доступным (scram) для локального инструментария, но сервис его не использует.

## Наружный доступ

Authentik слушает `127.0.0.1:<port>` — не публично. Наружу его выставляет Caddy-сайт `auth`
из [`profiles/tcp-gateway`](../../profiles/tcp-gateway/README.md): `http://auth.<node>.local/`
(LAN) и `http(s)://auth.<meshDomain>/` (mesh). Backend-порт в firewall **не** открывается.

## Ключевые опции

- `listenAddress` / `port` — bind (loopback).
- `domain` — публичный FQDN (влияет на redirect/root URL).
- `secretKeyFile` / `bootstrapTokenFile` / `bootstrapUserFile` / `bootstrapEmailFile` /
  `bootstrapPasswordFile` — agenix secret paths (см. «Секреты»).
- `dbUser` — OS-пользователь и роль БД (default `authentik`).
- `dataDir` — `/var/lib/authentik` (персистится через /persist).
- `forwardAuth` — защищаемые сайты; из списка генерируется автоматически применяемый Authentik
  Blueprint с provider/application/outpost state. Для разных LAN/mesh cookie domains создаются
  технические application-пары, но на Application Dashboard показывается только одна карточка
  сервиса; дополнительные transport applications получают `meta_hide = true`.
  После применения Blueprint one-shot unit дополнительно инвалидирует per-user application-куши
  (`app_access/*`). Иначе изменения `meta_hide` не доедут до Dashboard до конца 24-часового TTL:
  Authentik чистит этот куш только при **создании** Application, а Blueprint-запись с
  `state: present` обновляет существующий объект (тот же slug), а не создаёт.
- `oidcApplications` — OAuth2/OIDC-сервисы (`service`, `callbackPath`, client ID и runtime-путь
  к secret); LAN/mesh origins выводятся из общего gateway-контракта, а секрет читается
  Blueprint-тегом `!File`.
- `logLevel` — уровень логов.

## Использование

```nix
lattice.authentik = {
  enable = true;
  port = 9220;
  domain = "auth.mytecor-homelab.local";
  secretKeyFile = config.age.secrets.authentik-secret-key.path;
  bootstrapTokenFile = config.age.secrets.authentik-bootstrap-token.path;
  bootstrapUserFile = config.age.secrets.authentik-bootstrap-user.path;
  bootstrapEmailFile = config.age.secrets.authentik-bootstrap-email.path;
  bootstrapPasswordFile = config.age.secrets.authentik-bootstrap-password.path;
  forwardAuth = [ { service = "acp-ui"; } ];
};
```

Подробный контракт SSO описан в
[f14-01](../../roadmap/f14-sso-authentik/f14-01-deploy-authentik-sso.md).
