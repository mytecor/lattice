#!/usr/bin/env bash
#
# provision-authentik-acp-ui.sh — идемпотентный провижининг ForwardAuth для acp-ui.
#
# Часть [F14 — SSO через Authentik](../roadmap/f14-sso-authentik/README.md),
# реализует шаг 3 из [f14-02-provisioning.md](../roadmap/f14-sso-authentik/f14-02-provisioning.md):
# Caddy-директива forward_auth на loopback Authentik (127.0.0.1:9220, uri
# /outpost.goauthentik.io/auth/caddy) требует наличие ForwardAuth-провайдера +
# application в Authentik для КАЖДОГО host, на котором acp-ui обслуживается за этим
# `forward_auth`. Без подходящего провайдера подзапрос не находит приложение и
# не пропускает к статике SPA — отдаётся 404-страница Authentik.
#
# ВАЖНО (два независимых факта):
#
# 1. subrequest-путь forward-auth — /outpost.goauthentik.io/auth/caddy
#    (обслуживается встроенным outpost'ом Go-сервера), а НЕ устаревший
#    /akprox/auth/, который Django в этой версии больше не маршрутизирует и
#    отвечает 404.
#
# 2. Встроенный outpost сопоставляет подзапрос приложению СТРОГО по
#    X-Forwarded-Host/Host против external_host провайдера (mode=forward_single).
#    Один провайдер = один внешний host. Т.к. acp-ui обслуживается и на LAN
#    (http://acp-ui.<node>.local), и на mesh (https://acp-ui.<meshDomain>),
#    провайдер нужен на КАЖДЫЙ host: без отдельного mesh-провайдера подзапрос с
#    mesh-host не находит приложение, outpost отвечает 404, и Caddy отдаёт эту
#    404-страницу Authentik в браузер вместо статики SPA
#    («отсутствие статики, 404 от authentik» на https://acp-ui.<meshDomain>/).
#
# Скрипт также довносит системные flow входа/авторизации
# (default-authentication-flow и default-provider-authorization-explicit-consent),
# если они отсутствуют — они нужны провайдеру, а на свежеразвёрнутой ноде их
# может не быть (провижининг f14-02 не выполнялся).
#
# Запускать НА НОДЕ от root (там Authentik слушает loopback 127.0.0.1:9220, а agenix-
# секреты лежат на /run/agenix). Скрипт идемпотентен: каждый шаг сначала ищет объект
# по slug/name и создаёт, только если отсутствует; повторный запуск безопасен.
#
# Настройка hosts через переменные окружения (значения по умолчанию — LAN-only):
#   APP_SLUG      провайдер/application для LAN-host  (default acp-ui-fa)
#   APP_HOST      полный URL LAN-host                  (default http://acp-ui.mytecor-homelab.local)
#   COOKIE_DOMAIN cookie domain для LAN-провайдера      (default mytecor-homelab.local)
#   MESH_SLUG     провайдер/application для mesh-host   (default acp-ui-fa-mesh)
#   MESH_HOST     полный URL mesh-host, пусто = mesh не провижинится
#                 (пример: https://acp-ui.homelab.myt.su)
#   MESH_COOKIE_DOMAIN cookie domain для mesh-провайдера (пример: homelab.myt.su)
#
# Секреты (bootstrap-токен, SECRET_KEY) читаются только в переменные окружения и никогда
# не печатаются в stdout/лог.

set -euo pipefail

AK_BIN=${AK_BIN:-/nix/store/1g00imjh6xzxwj5fnqzd9din1kfabk1k-authentik-2026.5.6/bin/ak}
BLUEPRINTS_DIR=${BLUEPRINTS_DIR:-/nix/store/9appmk5y6f12ddbicpmjgx2wc96cki4z-python3.14-authentik-2026.5.6/blueprints/default}
AK_BASE_URL=${AK_BASE_URL:-http://127.0.0.1:9220/api/v3}
APP_SLUG=${APP_SLUG:-acp-ui-fa}
APP_HOST=${APP_HOST:-http://acp-ui.mytecor-homelab.local}
COOKIE_DOMAIN=${COOKIE_DOMAIN:-mytecor-homelab.local}
MESH_SLUG=${MESH_SLUG:-acp-ui-fa-mesh}
MESH_HOST=${MESH_HOST:-}
MESH_COOKIE_DOMAIN=${MESH_COOKIE_DOMAIN:-}
AUTH_FLOW_SLUG=${AUTH_FLOW_SLUG:-default-authentication-flow}
AUTHZ_FLOW_SLUG=${AUTHZ_FLOW_SLUG:-default-provider-authorization-explicit-consent}
INVALIDATION_FLOW_SLUG=${INVALIDATION_FLOW_SLUG:-default-provider-invalidation-flow}

# Обязательные системные blueprints, которые нужны провайдеру (проверить отсутствие и довнести).
AUTH_FLOW_BLUEPRINT=${AUTH_FLOW_BLUEPRINT:-flow-default-authentication-flow}
AUTHZ_FLOW_BLUEPRINT=${AUTHZ_FLOW_BLUEPRINT:-flow-default-provider-authorization-explicit-consent}

fail() {
  printf 'Provision failed: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || fail "required command is missing: $1"
}

read_secret() {
  # Читает значение переменной KEY=value из agenix-файла (EnvironmentFile-формат).
  # Значение печатается в stdout — поэтому вызывающий должен захватывать его в переменную,
  # а не давать попасть в консоль. Сама функция секрет не логирует.
  sed -n 's/^[[:space:]]*'"$1"'[[:space:]]*=[[:space:]]*//p' "$2"
}

ENV_NAME=${ENV_NAME:-AUTHENTIK_BOOTSTRAP_TOKEN}
TOKEN_FILE=${TOKEN_FILE:-/run/agenix/authentik-bootstrap-token}
SECRET_KEY_FILE=${SECRET_KEY_FILE:-/run/agenix/authentik-secret-key}

if [[ $EUID -ne 0 ]]; then
  fail "run this provisioner as root on the node"
fi
for command in curl jq sed runuser; do
  require_command "$command"
done
[[ -f "$TOKEN_FILE" ]] || fail "bootstrap token file missing: $TOKEN_FILE"
[[ -f "$SECRET_KEY_FILE" ]] || fail "secret key file missing: $SECRET_KEY_FILE"
[[ -x "$AK_BIN" ]] || fail "ak binary missing: $AK_BIN (set AK_BIN to the node store path)"
[[ -d "$BLUEPRINTS_DIR" ]] || fail "blueprints dir missing: $BLUEPRINTS_DIR (set BLUEPRINTS_DIR)"

# --- секреты в переменные, не в вывод ---
AK_TOKEN=$(read_secret AUTHENTIK_BOOTSTRAP_TOKEN "$TOKEN_FILE")
AK_SECRET_KEY=$(read_secret AUTHENTIK_SECRET_KEY "$SECRET_KEY_FILE")
[[ -n "$AK_TOKEN" ]] || fail "bootstrap token is empty"
[[ -n "$AK_SECRET_KEY" ]] || fail "secret key is empty"

ak_api() {
  curl -fsS -H "Authorization: Bearer $AK_TOKEN" "$@"
}

echo "[1/4] Проверяю системные flow входа/авторизации"
flow_exists() {
  local slug=$1
  local pk
  pk=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$slug" | jq -r '.results[0].pk // empty')
  [[ -n "$pk" ]]
}

AUTH_FLOW_PK=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$AUTH_FLOW_SLUG" | jq -r '.results[0].pk // empty')
if [[ -z "$AUTH_FLOW_PK" ]]; then
  echo "  flow '$AUTH_FLOW_SLUG' отсутствует — применяю системный blueprint"
  runuser -u authentik -- env \
    AUTHENTIK_POSTGRESQL__HOST=/run/postgresql \
    AUTHENTIK_POSTGRESQL__NAME=authentik \
    AUTHENTIK_POSTGRESQL__USER=authentik \
    AUTHENTIK_POSTGRESQL__SSLMODE=disable \
    AUTHENTIK_SECRET_KEY="$AK_SECRET_KEY" \
    "$AK_BIN" apply_blueprint "$BLUEPRINTS_DIR/$AUTH_FLOW_BLUEPRINT.yaml" >/dev/null
  AUTH_FLOW_PK=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$AUTH_FLOW_SLUG" | jq -r '.results[0].pk // empty')
  [[ -n "$AUTH_FLOW_PK" ]] || fail "authentication flow не создан после применения blueprint"
fi
echo "  authentication flow: $AUTH_FLOW_PK ($AUTH_FLOW_SLUG)"

if ! flow_exists "$AUTHZ_FLOW_SLUG"; then
  echo "  flow '$AUTHZ_FLOW_SLUG' отсутствует — применяю системный blueprint"
  runuser -u authentik -- env \
    AUTHENTIK_POSTGRESQL__HOST=/run/postgresql \
    AUTHENTIK_POSTGRESQL__NAME=authentik \
    AUTHENTIK_POSTGRESQL__USER=authentik \
    AUTHENTIK_POSTGRESQL__SSLMODE=disable \
    AUTHENTIK_SECRET_KEY="$AK_SECRET_KEY" \
    "$AK_BIN" apply_blueprint "$BLUEPRINTS_DIR/$AUTHZ_FLOW_BLUEPRINT.yaml" >/dev/null
fi
AUTHZ_FLOW_PK=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$AUTHZ_FLOW_SLUG" | jq -r '.results[0].pk // empty')
INVALIDATION_FLOW_PK=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$INVALIDATION_FLOW_SLUG" | jq -r '.results[0].pk // empty')
[[ -n "$AUTHZ_FLOW_PK" ]] || fail "authorization flow отсутствует: $AUTHZ_FLOW_SLUG"
[[ -n "$INVALIDATION_FLOW_PK" ]] || fail "invalidation flow отсутствует: $INVALIDATION_FLOW_SLUG"
echo "  authorization flow: $AUTHZ_FLOW_PK ($AUTHZ_FLOW_SLUG)"
echo "  invalidation flow: $INVALIDATION_FLOW_PK ($INVALIDATION_FLOW_SLUG)"

echo "[2/N] Проверяю ForwardAuth для каждого host acp-ui"
# provision_fa slug host cookie_domain: идемпотентно создаёт ForwardAuth-провайдера
# (mode=forward_single) + application под конкретный внешний host и проверяет, что
# subrequest /outpost.goauthentik.io/auth/caddy для этого host отвечает 302/200/401
# (не 404 — иначе Caddy отдаёт 404-страницу Authentik вместо статики SPA).
# external_host уникален на провайдера: один провайдер = один host (X-Forwarded-Host
# сопоставляется строго по external_host).
provision_fa() {
  local slug=$1 host=$2 cookie_domain=$3
  local host_name prov_pk app_pk http_code
  host_name=$(printf '%s' "$host" | sed -E 's#^[a-z]+://##; s#[:/].*$##')
  [ -n "$host_name" ] || fail "не удалось извлечь hostname из '$host'"

  echo "== ForwardAuth host: $host ($slug) =="
  PROV_PK=$(ak_api "$AK_BASE_URL/providers/proxy/?name=$slug" | jq -r '.results[0].pk // empty')
  if [[ -z "$PROV_PK" ]]; then
    echo "  провайдер отсутствует — создаю (mode=forward_single)"
    PROV_PK=$(curl -fsS -X POST -H "Authorization: Bearer $AK_TOKEN" -H "Content-Type: application/json" \
      -d "{
        \"name\": \"$slug\",
        \"authorization_flow\": \"$AUTH_FLOW_PK\",
        \"invalidation_flow\": \"$INVALIDATION_FLOW_PK\",
        \"mode\": \"forward_single\",
        \"external_host\": \"$host\",
        \"cookie_domain\": \"$cookie_domain\",
        \"invalidate_sessions_on_logout\": true,
        \"basic_auth_enabled\": false
      }" "$AK_BASE_URL/providers/proxy/" | jq -r '.pk')
    [[ -n "$PROV_PK" ]] || fail "не удалось создать proxy-провайдера для $host"
  fi
  echo "  proxy provider: $PROV_PK ($slug)"

  APP_PK=$(ak_api "$AK_BASE_URL/core/applications/?slug=$slug" | jq -r '.results[0].pk // empty')
  if [[ -z "$APP_PK" ]]; then
    echo "  application отсутствует — создаю с привязкой к провайдеру"
    APP_PK=$(curl -fsS -X POST -H "Authorization: Bearer $AK_TOKEN" -H "Content-Type: application/json" \
      -d "{
        \"name\": \"$slug\",
        \"slug\": \"$slug\",
        \"provider\": $PROV_PK,
        \"meta_launch_url\": \"$host/\"
      }" "$AK_BASE_URL/core/applications/" | jq -r '.pk')
    [[ -n "$APP_PK" ]] || fail "не удалось создать application для $host"
  fi
  echo "  application: $APP_PK ($slug)"

  # Проверка subrequest forward_auth от Caddy на loopback Authentik; неаутентифицированный
  # запрос должен получить 302 на форму входа Authentik (не 404/500). Встроенный outpost
  # Go-сервера определяет приложение по X-Forwarded-Host (+ Host), поэтому проба идёт с
  # этими шапками, без query-параметров; X-Forwarded-Proto берётся из схемы host.
  local proto=${host%%://*}
  http_code=$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Host: $host_name" \
    -H "X-Forwarded-Host: $host_name" \
    -H "X-Forwarded-Proto: $proto" \
    "http://127.0.0.1:9220/outpost.goauthentik.io/auth/caddy")
  echo "  /outpost.goauthentik.io/auth/caddy ($host) -> HTTP $http_code"
  if [[ "$http_code" != "302" && "$http_code" != "200" && "$http_code" != "401" ]]; then
    fail "endpoint /outpost.goauthentik.io/auth/caddy для $host ответил $http_code (ожидался 302/200/401) — провайдер не подхватился"
  fi
}

# LAN-host — всегда.
provision_fa "$APP_SLUG" "$APP_HOST" "$COOKIE_DOMAIN"

# mesh-host — только если оператор указал MESH_HOST (иначе mesh не провижинится и
# subrequest с mesh-host не найдёт приложение → 404 Authentik вместо статики SPA).
if [[ -n "$MESH_HOST" ]]; then
  [[ -n "$MESH_COOKIE_DOMAIN" ]] || fail "MESH_HOST задан, но MESH_COOKIE_DOMAIN пуст — укажите cookie domain для mesh-провайдера"
  provision_fa "$MESH_SLUG" "$MESH_HOST" "$MESH_COOKIE_DOMAIN"
fi

echo "Готово. ForwardAuth для acp-ui провижинен на hosts: '$APP_HOST'${MESH_HOST:+", '$MESH_HOST'"}."
