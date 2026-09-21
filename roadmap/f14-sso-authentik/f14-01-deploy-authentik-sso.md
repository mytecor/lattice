# f14-01. Развернуть Authentik нативно через NixOS и встроить как центральный SSO

Фича: [F14 — SSO через Authentik](./README.md). Зависит от
[F4](../f4-payload/README.md) (прикладной HTTP ingress через Caddy
[`tcp-gateway`](../../profiles/tcp-gateway/README.md)) и от конвенций секретов
[F2](../f2-secrets-identity/README.md) (agenix, секреты только runtime-файлами).

## Контекст

На ноде уже есть единый внешний listener — Caddy из
[`profiles/tcp-gateway`](../../profiles/tcp-gateway/README.md), — через который доступны
пользовательские web-сервисы: `http://grafana.<node>.local/`,
`http://acp-ui.<node>.local/`, `http://status.<node>.local/` и mesh-адреса
`https://<service>.<meshDomain>/`. До сих пор каждый сервис защищается по-своему: у Grafana —
admin-пароль из agenix, у статических сайтов защиты нет вовсе. Нужен один декларативный механизм
«кто открывает интерфейс».

Проверено по закреплённому nixpkgs (коммит `dc5d91f8`):

- пакет `pkgs.authentik` **доступен** (версия `2026.5.6`), включает wrapper `ak` с подкомандами
  `server` / `worker` / `manage` / `healthcheck` (см. `bin/.ak-wrapped`) — поверх него можно
  декларативно строить `server` + `worker` systemd-юниты;
- NixOS-модуля `services.authentik` в закреплённом nixpkgs **нет** — модуль пишем сами
  (`modules/authentik/`), по образцу остальных Lattice-модулей: типизированные опции,
  `default.nix` + `options.nix`, agenix-секреты как runtime-пути, никаких секретов в store;
- в пакете отдельными store-путями собраны `authentik-proxy` и `authentik-worker`, но для
  ForwardAuth (шаг 6) используется Caddy `forward_auth` против готового
  `/outpost.goauthentik.io/auth/caddy` endpoint встроенного outpost'а Go-сервера, а не
  самосборный outpost.

## Что сделать

- [ ] 1. **Модуль `modules/authentik/`**: `default.nix` + `options.nix` (типизированные опции по
      образцу [`modules/grafana`](../../modules/grafana/README.md)). Поверх `services.postgresql`
      и `services.redis`; systemd-юниты `authentik-server` и `authentik-worker` через
      `${pkgs.authentik}/bin/ak server` / `ak worker`; `manage migrate` в `preStart`/one-shot;
      `/var/lib/authentik` персистится через impermanence (как
      [`/var/lib/grafana`](../../nodes/mytecor-homelab/config.nix)).
- [ ] 2. **Секреты через agenix** (`nodes/mytecor-homelab/secrets/secrets.nix` + `.age`-файлы):
      `authentik-secret-key` (`SECRET_KEY`), `authentik-bootstrap-token`, пароль выделенного
      DB-пользователя (`authentik-postgres-password`) и при необходимости stack-пароль. Все — с
      получателями `admin` + `node` в `publicKeys`, как у существующих. В store попадают только
      runtime-пути, не значения.
- [ ] 3. **Flake-интеграция**: input `module-authentik = { url = "path:./modules/authentik"; }`,
      экспорт `nixosModules.authentik`, включение в `self.nixosModules.default`.
- [ ] 4. **Caddy-ингресс для самого Authentik**: сайт `auth` на LAN `http://auth.<node>.local/`
      и на mesh `https://auth.<meshDomain>/` через `serviceSites` из
      [`profiles/tcp-gateway/config.nix`](../../profiles/tcp-gateway/config.nix); mDNS-публикатор
      `auth-mdns` по образцу `grafana-mdns`. Authentik слушает loopback, backend-порт не в
      firewall (как Grafana). `auth` НЕ в `meshExclude` — это логинстраница, должна быть
      достижима с mesh-клиентов.
- [ ] 5. **Bootstrap-провижиниг** (декларативно, источник истины — репозиторий, не клики в UI):
      оператор-аккаунт, default flow, OIDC-провайдер и ForwardAuth endpoint для последующих
      подключений. Скрипт через `ak manage shell`/REST, bootstrap-token из agenix; идемпотентный.
- [ ] 6. **Caddy ForwardAuth для сервисов без собственного SSO**: опция профиля
      `tcp-gateway`/общий шаблон, которая оборачивает `extraConfig` уже существующего сайта
      (`serviceSites`) директивой `forward_auth` + `auth_request` на
      `/outpost.goauthentik.io/auth/caddy`, чтобы не трогать backend. Применение к статике
      (`acp-ui`, `status`) — по решению оператора; проверка,
      что `acp-ui` за ActuallyAuthenticate защищает браузерный UI, не ломая WebSocket-контракт
      с backend (формы соединения f8-06 не меняются). ForwardAuth-провайдер создаётся **на
      каждый host** (LAN и mesh): outpost сопоставляет приложение строго по
      `X-Forwarded-Host`/`Host` против `external_host` (один провайдер = один host) — без
      mesh-провайдера mesh-сайт отдаёт 404-страницу Authentik вместо статики (см.
      [f14-02](./f14-02-provisioning.md#3-forwardauth-endpoint-для-acp-ui-статический-web-клиент-acp)).
- [ ] 7. **Нативный OIDC: Grafana** — в [`modules/grafana`](../../modules/grafana/README.md)
      секция `auth.generic_oauth` (client_id/client_secret/`auth_url`/`token_url`/`api_url`,
      scopes `openid profile email`), client-секрет — agenix. Вход через Authentik, admin-роль
      мапится из группы/claim. Логинстраница Authentik остаётся единственной точкой входа.
- [ ] 8. **Machine-to-machine не завязывается на browser-based flow**: API-пути/сервисы (LLM
      gateway API-ключи, Radicle-подписи, rnsh/rns, ACP daemon) остаются как есть; SSO защищает
      только браузерный UI. Для API-сервиса с раздельными UI/API (например `acp-ui` против
      `acp`) — защищается только UI-сайт, API endpoints не трогаются: UI-ставка за Caddy
      `forward_auth` с `uri_prune`/subpath, backend-контракт без изменений.
- [ ] 9. **Контракт-тест** `tests/authentik.nix` (по образцу
      [`tests/grafana-ingress.nix`](../../tests/grafana-ingress.nix)): модуль собирается при
      заданных секретах; assertion без секретов падает; Caddy-сайт `auth` существует и
      проксирует на loopback; backend-порт не в firewall; `auth-mdns` публикует alias; Authentik
      binds 127.0.0.1; у ForwardAuth-сайтов `extraConfig` содержит `forward_auth` на
      `/outpost.goauthentik.io/auth/caddy`; Grafana-конфиг содержит `auth.generic_oauth`. Плюс
      `nix flake check`.
- [ ] 10. **Живое подтверждение** на `mytecor-homelab`: вход в Authentik (bootstrap-token →
      оператор), создание провайдера, вход Grafana через SSO, проверка ForwardAuth-сайта,
      достижимость `auth` на LAN и mesh. Зафиксировать результат в этой задаче.

## Критерий готовности (Definition of Done)

- [ ] Authentik развёрнут декларативно (module + flake), читает секреты только из agenix
      runtime-файлов; ни один секрет не в Nix store (проверяется).
- [ ] `auth.<node>.local` (LAN) и `auth.<meshDomain>` (mesh) отвечают; вход оператором работает;
      provisioning идемпотентен и живёт в репозитории.
- [ ] Сервис без собственного SSO защищён Caddy ForwardAuth; Grafana входит через OIDC-провайдер
      Authentik — один вход открывает оба интерфейса.
- [ ] M2M/API-пути не зависят от браузерной сессии Authentik; у API-сервиса с раздельными UI/API
      защищён только UI, API endpoints не тронуты (подтверждено тестом/проверкой).
- [ ] `nix flake check --all-systems --no-build` зелёный (включая новый контракт-тест).

## Затрагиваемые файлы / слои

- `modules/authentik/` (новый) — `default.nix`, `options.nix`, `README.md`.
- [`flake.nix`](../../flake.nix) — input `module-authentik`, `nixosModules.authentik`,
  `nixosModules.default`.
- [`profiles/tcp-gateway/config.nix`](../../profiles/tcp-gateway/config.nix) — Caddy-сайты
  `auth` + ForwardAuth, mDNS-публикатор `auth-mdns`.
- [`modules/grafana/config.nix`](../../modules/grafana/README.md) — секция `auth.generic_oauth`.
- [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/README.md) — включение модуля,
  `/var/lib/authentik` в `/persist`, agenix-секреты.
- [`nodes/mytecor-homelab/secrets/secrets.nix`](../../nodes/mytecor-homelab/README.md) —
  получатели новых `.age`-секретов.
- [`tests/`](../../tests/README.md) — `tests/authentik.nix`.
- [`ROADMAP.md`](../../ROADMAP.md) — веха F14.

## Открытые вопросы

- **СУБД**: системный `services.postgresql` (одна БД на ноду) или отдельный кластер для
  Authentik. Решение по ходу шага 1; контракт — пароль только agenix.
- **Bootstrap-токен**: временный (после provisioning отзывается) против постоянного.
  Предпочтительно временный.
- **mesh и `auth`**: вход с mesh-клиентов нужен, поэтому `auth` не в `meshExclude`.
- **какие именно статические сайты** (`acp-ui`, `status`) заворачиваются в ForwardAuth на первом
  шаге — по решению оператора; механизм делается общим, без изменения backend.

_нет_ открытых блокеров до старта.
