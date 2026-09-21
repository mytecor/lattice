# f14-02. Provisioning: flows, OIDC (Grafana), ForwardAuth

Часть [F14 — SSO через Authentik](./README.md), продолжает
[f14-01](./f14-01-deploy-authentik-sso.md) (развёртывание/встраивание). Здесь — декларативный
провижининг **после** того, как Authentik поднят на ноде (шаг 5 в f14-01): источник истины —
репозиторий, а не клики в UI.

> Статус: **подготовлено декларативно, на живой ноде НЕ выполнялось** (по решению оператора:
> разворачиваем всё декларативно, живое подтверждение — отдельный шаг в f14-01). Скрипты/шаги
> ниже — воспроизводимая инструкция для оператора; реальные slug/id могут отличаться в
> зависимости от версии и ручного вмешательства — проверяйте ответы API.

## Принципы

- **Вход один общий**: логин в Authentik открывает защищённый веб-UI всех подключённых сервисов.
- **Ничего в Nix store**: секреты (API-токен из `authentik-bootstrap-token`, client_secret из
  `grafana-oauth-client-secret`) читаются только с runtime-путей agenix и передаются в
  `curl` через переменные окружения — не в командной строке и не в вывод.
- **Идемпотентность**: каждый шаг сначала ищет объект по известному `slug`/`name`, создаёт
  только если отсутствует.
- Токен постоянный (`intent=api, expiring=false`, из `AUTHENTIK_BOOTSTRAP_TOKEN`).

## 0. Доступ к API

Запускать **на ноде** (там Authentik слушает loopback 127.0.0.1:9220, но сайт `auth`
достижим и с Mac по LAN/mesh). Токен читаем из расшифрованного agenix-секрета, не печатая:

```sh
# на ноде, от оператора (sudo): расшифровать один раз, держать в переменной
read -r AUTHENTIK_BOOTSTRAP_TOKEN < <(sudo cat /run/agenix/authentik-bootstrap-token)
# строка вида AUTHENTIK_BOOTSTRAP_TOKEN=<token>; срезаем префикс:
AK_TOKEN="${AUTHENTIK_BOOTSTRAP_TOKEN#*=}"
AK_BASE="http://127.0.0.1:9220/api/v3"
alias ak-api='curl -fsS -H "Authorization: Bearer $AK_TOKEN"'
```

Секрет client_secret для Grafana (шаг 2) — так же, из
`/run/agenix/grafana-oauth-client-secret` (там лежит **только** hex-значение, без префикса).

## 1. Default flow (страница входа)

Identity-провайдер default создаётся bootstrap'ом; удостоверьтесь, что flow входа на месте:

```sh
ak-api "$AK_BASE/flows/instances/?slug=default-authentication-flow" \
  | jq -r '.results[0].pk'
```

Если пусто — создайте (но обычно есть по умолчанию; этот шаг — идемпотентная проверка):

```sh
ak-api -X POST "$AK_BASE/flows/instances/" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Default Authentication Flow",
    "slug": "default-authentication-flow",
    "title": "Sign in",
    "designation": "authentication"
  }'
```

## 2. OIDC provider для Grafana

Grafana подключается нативно (f14-01, шаг 7): модуль читает `authUrl`/`tokenUrl`/`apiUrl`
`http://auth.<node>.local/application/o/{authorize,token,userinfo}/`, `client_id=grafana` и
client_secret из agenix. Создаём провайдера (идемпотентно по имени) в Authentik:

```sh
AK_GRAFANA_CLIENT_SECRET=$(sudo cat /run/agenix/grafana-oauth-client-secret)
# если ещё нет провайдера "grafana":
ak-api -X POST "$AK_BASE/providers/oauth2/" \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\": \"grafana\",
    \"client_type\": \"confidential\",
    \"client_id\": \"grafana\",
    \"client_secret\": \"$AK_GRAFANA_CLIENT_SECRET\",
    \"authorization_flow\": \"$(ak-api "$AK_BASE/flows/instances/?slug=default-provider-authorization-explicit-consent" | jq -r '.results[0].pk')\",
    \"redirect_uris\": [\"http://grafana.mytecor-homelab.local/login/generic_oauth\"],
    \"signing_key\": \"\",
    \"access_code_validity\": \"minutes=1\",
    \"access_token_validity\": \"minutes=5\",
    \"refresh_token_validity\": \"days=30\",
    \"sub_mode\": \"hashed_user_id\",
    \"jwks_sources\": \"\"
  }"
```

Затем — application, привязывающий провайдера (нужен, чтобы пользователь увидел вход и чтобы
Grafana могла по нему аутентифицироваться):

```sh
OIDC_PK=$(ak-api "$AK_BASE/providers/oauth2/?name=grafana" | jq -r '.results[0].pk')
ak-api -X POST "$AK_BASE/core/applications/" \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\": \"grafana\",
    \"slug\": \"grafana\",
    \"provider\": $OIDC_PK,
    \"meta_launch_url\": \"http://grafana.mytecor-homelab.local/\"
  }"
```

## 3. ForwardAuth endpoint для acp-ui (статический web-клиент ACP)

Caddy-директива в `profiles/app-services/config.nix` для сайта `acp-ui` использует `forward_auth`
на loopback Authentik `http://127.0.0.1:9220` с `uri` по умолчанию
`/outpost.goauthentik.io/auth/caddy` (или `entry.uri` из `lattice.authentik.forwardAuth`). Authentik
должен иметь ForwardAuth-провайдера + application **на каждый host**, на котором acp-ui
обслуживается за этим `forward_auth` (LAN и mesh).

> **Почему провайдер на каждый host.** Встроенный outpost сопоставляет подзапрос приложению
> СТРОГО по `X-Forwarded-Host`/`Host` против `external_host` провайдера (`mode=forward_single`):
> один провайдер = один внешний host. acp-ui обслуживается и на LAN
> (`http://acp-ui.<node>.local`), и на mesh (`https://acp-ui.<meshDomain>`). Без отдельного
> mesh-провайдера подзапрос с mesh-host не находит приложение, outpost отвечает **404**, и Caddy
> отдаёт эту 404-страницу Authentik в браузер вместо статики SPA — приём «отсутствие статики,
> 404 от authentik» на `https://acp-ui.<meshDomain>/`.

Рекомендуемый путь — идемпотентный скрипт
[`scripts/provision-authentik-acp-ui.sh`](../../scripts/provision-authentik-acp-ui.sh): на каждый
host создаёт провайдера + application (slug по умолчанию `acp-ui-fa` для LAN, `acp-ui-fa-mesh` для
mesh) и проверяет, что subrequest `/outpost.goauthentik.io/auth/caddy` для этого host отвечает
302/200/401 (не 404). Запускать на ноде от root; mesh-провайдер включается переменной `MESH_HOST`:

```sh
# LAN + mesh (mesh — внешний yggdrasil-доступ):
MESH_HOST=https://acp-ui.homelab.myt.su \
MESH_COOKIE_DOMAIN=homelab.myt.su \
  ./scripts/provision-authentik-acp-ui.sh

# или только LAN (mesh не провижинится):
./scripts/provision-authentik-acp-ui.sh
```

Эквивалент вручную — на каждый host свой провайдер (ниже LAN-пример):

```sh
# flow, который перехватывает неаутентифицированный запрос (шаг 6 в f14-01):
FA_FLOW=$(ak-api "$AK_BASE/flows/instances/?slug=default-authentication-flow" | jq -r '.results[0].pk')

# провайдер-cproxy (forward auth), slug/имя "acp-ui-fa" (LAN-host):
ak-api -X POST "$AK_BASE/providers/proxy/" \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\": \"acp-ui-fa\",
    \"authorization_flow\": \"$FA_FLOW\",
    \"mode\": \"forward_single\",
    \"external_host\": \"http://acp-ui.mytecor-homelab.local\",
    \"cookie_domain\": \"mytecor-homelab.local\",
    \"invalidate_sessions_on_logout\": true,
    \"basic_auth_enabled\": false
  }"

# для mesh — второй провайдер "acp-ui-fa-mesh" с external_host https://acp-ui.homelab.myt.su
# и cookie_domain homelab.myt.su (свой на каждый host).
```

Затем application-объекты (по одному на провайдера, slug/имя совпадают с провайдером, аналогично
шагу 2). После этого `forward_auth` Caddy будет возвращать 302 на логин Authentik для
неаутентифицированных браузерных запросов на каждом из hosts.

> Примечание. `lattice.authentik.forwardAuth[].uri` по умолчанию
> `/outpost.goauthentik.io/auth/caddy` — подзапрос, который обслуживает встроенный outpost
> Go-сервера Authentik (определяет приложение по `X-Forwarded-*`/`Host`, возвращает 302 → логин
> для неаутентифицированных запросов). Устаревший `/akprox/auth/` в этой версии (2026.5.6)
> Django больше не маршрутизирует и отвечает 404, поэтому как дефолт он не годен. Если оператор
> запускает собственный outpost на отдельном пути, укажите его в опции `uri` (легальная настройка).
> Тот же механизм «один провайдер на host» не меняется от выбора `uri`.

## 4. Проверка

```sh
# страница входа (шаг 6 f14-01 уже требовал это):
curl --fail http://auth.mytecor-homelab.local/if/flow/initial/
# ForwardAuth защищает браузерный UI acp-ui (до входа — редирект/401):
curl -s -o /dev/null -w '%{http_code}\n' http://acp-ui.mytecor-homelab.local/
# Grafana редиректит на auth:
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' http://grafana.mytecor-homelab.local/login
```

Полностью проверить вход (Authentik page → Grafana с admin-ролью) — вручную в браузере:
логин `akadmin` + bootstrap password (из `authentik-bootstrap-password`).

## Затрагиваемые файлы / слои

- [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix) — forwardAuth,
  grafana.oauth (URL-контракты `/application/o/...`).
- [`modules/authentik/README.md`](../../modules/authentik/README.md) — bootstrap/секреты.
- Authentik REST API — [`docs.goauthentik.io/docs/api`](https://docs.goauthentik.io/docs/api).
