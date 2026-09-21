#!/usr/bin/env bash
#
# provision-authentik-grafana.sh — идемпотентный провижининг нативного OIDC для Grafana.
#
# Часть [F14 — SSO через Authentik](../roadmap/f14-sso-authentik/README.md),
# реализует шаг 2 из [f14-02-provisioning.md](../roadmap/f14-sso-authentik/f14-02-provisioning.md):
# Grafana подключается к Authentik как OIDC Relying Party (client_id=grafana), а не через
# ForwardAuth. Для этого в Authentik нужен OAuth2-провайдер (client_type=confidential,
# client_id=grafana, client_secret из agenix) + application (slug=grafana).
#
# БЕЗ этого провайдера:
#   * Grafana нет в списке сервисов на странице входа Authentik (нет application);
#   * вход через «Login with Authentik» падает на authorize-endpoint ошибкой
#     «Client ID Error — The client identifier (client_id) is missing or invalid»
#     (нет provider с client_id=grafana).
#
# Grafana формирует redirect_uri как <root_url>/login/generic_oauth, где root_url — внешний
# URL Grafana (lattice.grafana.domain, mesh-canonical https://grafana.<meshDomain>). Поэтому
# ПРОВАЙДЕР должен разрешать redirect_uri на КАЖДЫЙ внешний хост, через который оператор
# ходит в Grafana: LAN (http://grafana.<node>.local) и mesh (https://grafana.<meshDomain>).
# Скрипт регистрирует оба (LAN всегда; mesh — только если задан MESH_HOST).
#
# Запускать НА НОДЕ от root (там Authentik слушает loopback 127.0.0.1:9220, а agenix-секреты
# лежат на /run/agenix). Скрипт идемпотентен: каждый шаг сначала ищет объект по имени/slug
# и создаёт, только если отсутствует; повторный запуск безопасен.
#
# Настройка hosts через переменные окружения (значения по умолчанию — LAN-хосты ноды):
#   LAN_HOST      full URL LAN-хоста Grafana          (default http://grafana.mytecor-homelab.local)
#   MESH_HOST     full URL mesh-хоста, пусто = mesh не провижинится
#                 (пример: https://grafana.homelab.myt.su)
#   META_URL      meta_launch_url application (default = LAN_HOST)
#
# Секреты (bootstrap-токен, SECRET_KEY, client_secret Grafana) читаются только в переменные
# окружения и никогда не печатаются в stdout/лог. client_secret файла grafana-oauth-client-secret
# содержит ГОЛОЕ hex-значение (без префикса KEY=), в отличие от authentik-секретов.

set -euo pipefail

AK_BIN=${AK_BIN:-/nix/store/1g00imjh6xzxwj5fnqzd9din1kfabk1k-authentik-2026.5.6/bin/ak}
BLUEPRINTS_DIR=${BLUEPRINTS_DIR:-/nix/store/9appmk5y6f12ddbicpmjgx2wc96cki4z-python3.14-authentik-2026.5.6/blueprints/default}
AK_BASE_URL=${AK_BASE_URL:-http://127.0.0.1:9220/api/v3}

# --- hosts ---
LAN_HOST=${LAN_HOST:-http://grafana.mytecor-homelab.local}
MESH_HOST=${MESH_HOST:-}
# meta_launch_url application: mesh-хост приоритетнее (mesh-canonical), иначе LAN.
META_URL=${META_URL:-${MESH_HOST:-$LAN_HOST}}

# --- flows (как в acp-ui-провижинере) ---
AUTH_FLOW_SLUG=${AUTH_FLOW_SLUG:-default-authentication-flow}
AUTHZ_FLOW_SLUG=${AUTHZ_FLOW_SLUG:-default-provider-authorization-explicit-consent}
INVALIDATION_FLOW_SLUG=${INVALIDATION_FLOW_SLUG:-default-provider-invalidation-flow}
AUTH_FLOW_BLUEPRINT=${AUTH_FLOW_BLUEPRINT:-flow-default-authentication-flow}
AUTHZ_FLOW_BLUEPRINT=${AUTHZ_FLOW_BLUEPRINT:-flow-default-provider-authorization-explicit-consent}

# --- секреты ---
TOKEN_FILE=${TOKEN_FILE:-/run/agenix/authentik-bootstrap-token}
SECRET_KEY_FILE=${SECRET_KEY_FILE:-/run/agenix/authentik-secret-key}
GRAFANA_SECRET_FILE=${GRAFANA_SECRET_FILE:-/run/agenix/grafana-oauth-client-secret}

fail() {
  printf 'Provision failed: %s\n' "$1" >&2
  exit 1
}
require_command() {
  command -v "$1" >/dev/null || fail "required command is missing: $1"
}
read_env_secret() {
  # Значение KEY=value из agenix-файла (EnvironmentFile-формат). Вызывающий захватывает
  # в переменную; сама функция секрет не логирует.
  sed -n 's/^[[:space:]]*'"$1"'[[:space:]]*=[[:space:]]*//p' "$2"
}

if [[ $EUID -ne 0 ]]; then
  fail "run this provisioner as root on the node"
fi
for command in curl jq sed runuser; do
  require_command "$command"
done
[[ -f "$TOKEN_FILE" ]] || fail "bootstrap token file missing: $TOKEN_FILE"
[[ -f "$SECRET_KEY_FILE" ]] || fail "secret key file missing: $SECRET_KEY_FILE"
[[ -f "$GRAFANA_SECRET_FILE" ]] || fail "grafana client secret file missing: $GRAFANA_SECRET_FILE"
[[ -x "$AK_BIN" ]] || fail "ak binary missing: $AK_BIN (set AK_BIN to the node store path)"
[[ -d "$BLUEPRINTS_DIR" ]] || fail "blueprints dir missing: $BLUEPRINTS_DIR (set BLUEPRINTS_DIR)"

# --- секреты в переменные, не в вывод ---
AK_TOKEN=$(read_env_secret AUTHENTIK_BOOTSTRAP_TOKEN "$TOKEN_FILE")
AK_SECRET_KEY=$(read_env_secret AUTHENTIK_SECRET_KEY "$SECRET_KEY_FILE")
# Файл grafana-oauth-client-secret — ГОЛОЕ hex-значение (без KEY=). Берём целиком, обрезая
# пробелы/перевод строки. Подстановка в curl идёт через переменную, не в командную строку.
GRAFANA_CLIENT_SECRET=$(tr -d '[:space:]' < "$GRAFANA_SECRET_FILE")
[[ -n "$AK_TOKEN" ]] || fail "bootstrap token is empty"
[[ -n "$AK_SECRET_KEY" ]] || fail "secret key is empty"
[[ -n "$GRAFANA_CLIENT_SECRET" ]] || fail "grafana client secret is empty"

ak_api() {
  curl -fsS -H "Authorization: Bearer $AK_TOKEN" "$@"
}

echo "[1/4] Проверяю системные flow входа/авторизации"
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

AUTHZ_FLOW_PK=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$AUTHZ_FLOW_SLUG" | jq -r '.results[0].pk // empty')
if [[ -z "$AUTHZ_FLOW_PK" ]]; then
  echo "  flow '$AUTHZ_FLOW_SLUG' отсутствует — применяю системный blueprint"
  runuser -u authentik -- env \
    AUTHENTIK_POSTGRESQL__HOST=/run/postgresql \
    AUTHENTIK_POSTGRESQL__NAME=authentik \
    AUTHENTIK_POSTGRESQL__USER=authentik \
    AUTHENTIK_POSTGRESQL__SSLMODE=disable \
    AUTHENTIK_SECRET_KEY="$AK_SECRET_KEY" \
    "$AK_BIN" apply_blueprint "$BLUEPRINTS_DIR/$AUTHZ_FLOW_BLUEPRINT.yaml" >/dev/null
  AUTHZ_FLOW_PK=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$AUTHZ_FLOW_SLUG" | jq -r '.results[0].pk // empty')
fi
INVALIDATION_FLOW_PK=$(ak_api "$AK_BASE_URL/flows/instances/?slug=$INVALIDATION_FLOW_SLUG" | jq -r '.results[0].pk // empty')
[[ -n "$AUTHZ_FLOW_PK" ]] || fail "authorization flow отсутствует: $AUTHZ_FLOW_SLUG"
[[ -n "$INVALIDATION_FLOW_PK" ]] || fail "invalidation flow отсутствует: $INVALIDATION_FLOW_SLUG"
echo "  authorization flow: $AUTHZ_FLOW_PK ($AUTHZ_FLOW_SLUG)"
echo "  invalidation flow: $INVALIDATION_FLOW_PK ($INVALIDATION_FLOW_SLUG)"

# redirect_uri Grafana = <root_url>/login/generic_oauth на КАЖДЫЙ внешний хост.
# Authentik 2026.5.x принимает redirect_uris как список ОБЪЕКТОВ
# (RedirectURIRequest): {"matching_mode", "url", "redirect_uri_type"}, а не голые строки.
redirect_uri_entry() {
  local host=$1
  printf '{"matching_mode": "strict", "url": "%s", "redirect_uri_type": "authorization"}' \
    "$host/login/generic_oauth"
}
REDIRECT_LAN=$(redirect_uri_entry "$LAN_HOST")

echo "[2/4] Собираю список redirect_uris OIDC-провайдера Grafana"
REDIRECT_URIS="[$REDIRECT_LAN"
if [[ -n "$MESH_HOST" ]]; then
  REDIRECT_URIS="$REDIRECT_URIS, $(redirect_uri_entry "$MESH_HOST")"
fi
REDIRECT_URIS="$REDIRECT_URIS]"
echo "  redirect_uris: $REDIRECT_URIS"

echo "[3/4] Проверяю OAuth2-провайдера grafana"
PROV_PK=$(ak_api "$AK_BASE_URL/providers/oauth2/?name=grafana" | jq -r '.results[0].pk // empty')
if [[ -z "$PROV_PK" ]]; then
  echo "  провайдер отсутствует — создаю (client_type=confidential, client_id=grafana)"
  PROV_PK=$(curl -fsS -X POST -H "Authorization: Bearer $AK_TOKEN" -H "Content-Type: application/json" \
    -d "{
      \"name\": \"grafana\",
      \"client_type\": \"confidential\",
      \"client_id\": \"grafana\",
      \"client_secret\": \"$GRAFANA_CLIENT_SECRET\",
      \"authorization_flow\": \"$AUTHZ_FLOW_PK\",
      \"invalidation_flow\": \"$INVALIDATION_FLOW_PK\",
      \"redirect_uris\": $REDIRECT_URIS,
      \"grant_types\": [\"authorization_code\", \"refresh_token\"],
      \"signing_key\": \"\",
      \"access_code_validity\": \"minutes=1\",
      \"access_token_validity\": \"minutes=5\",
      \"refresh_token_validity\": \"days=30\",
      \"sub_mode\": \"hashed_user_id\",
      \"jwks_sources\": \"\"
    }" "$AK_BASE_URL/providers/oauth2/" | jq -r '.pk')
  [[ -n "$PROV_PK" ]] || fail "не удалось создать OAuth2-провайдера grafana"
  echo "  создан provider: $PROV_PK"
else
  echo "  провайдер уже есть: $PROV_PK — обновляю redirect_uris и grant_types (идемпотентно)"
  curl -fsS -X PATCH -H "Authorization: Bearer $AK_TOKEN" -H "Content-Type: application/json" \
    -d "{\"redirect_uris\": $REDIRECT_URIS, \"grant_types\": [\"authorization_code\", \"refresh_token\"]}" \
    "$AK_BASE_URL/providers/oauth2/$PROV_PK/" >/dev/null
fi

echo "[4/4] Проверяю application grafana"
# Authentik 2026.5.x: detail-эндпоинт applications ключуется по slug (не UUID),
# а query-фильтр ?slug= возвращает ВСЕ приложения (не фильтрует). Поэтому ищем
# application grafana явным select по slug и PATCH-им по slug-пути.
APP_SLUG=$(ak_api "$AK_BASE_URL/core/applications/" | jq -r 'first(.results[] | select(.slug=="grafana") | .slug // empty)' | cat)
if [[ -z "$APP_SLUG" ]]; then
  echo "  application отсутствует — создаю с привязкой к провайдеру"
  APP_PK=$(curl -fsS -X POST -H "Authorization: Bearer $AK_TOKEN" -H "Content-Type: application/json" \
    -d "{
      \"name\": \"grafana\",
      \"slug\": \"grafana\",
      \"provider\": $PROV_PK,
      \"meta_launch_url\": \"$META_URL/\"
    }" "$AK_BASE_URL/core/applications/" | jq -r '.pk')
  [[ -n "$APP_PK" ]] || fail "не удалось создать application grafana"
  APP_SLUG=grafana
  echo "  создан application: $APP_PK (slug=grafana)"
else
  # Если application уже есть, но привязан к другому/никакому провайдеру или URL устарел —
  # обновляем привязку, чтобы он точно смотрел на провайдера grafana. Путь — по slug.
  curl -fsS -X PATCH -H "Authorization: Bearer $AK_TOKEN" -H "Content-Type: application/json" \
    -d "{\"provider\": $PROV_PK, \"meta_launch_url\": \"$META_URL/\"}" \
    "$AK_BASE_URL/core/applications/$APP_SLUG/" >/dev/null
  echo "  application уже есть: $APP_SLUG (привязка к провайдеру обновлена)"
fi

echo
echo "Проверка:"
# Провайдер виден по имени; client_id подтверждаем, что зарегистрирован grafana.
CONF_CLIENT_ID=$(ak_api "$AK_BASE_URL/providers/oauth2/$PROV_PK/" | jq -r '.client_id // empty')
if [[ "$CONF_CLIENT_ID" != "grafana" ]]; then
  fail "client_id провайдера = '$CONF_CLIENT_ID', ожидался 'grafana'"
fi
echo "  OAuth2 provider grafana: pk=$PROV_PK client_id=$CONF_CLIENT_ID"
echo "  application grafana: $APP_SLUG (keyed by slug)"
echo "  redirect_uris: $REDIRECT_URIS"
echo "Готово. Grafana видна в списке сервисов Authentik; вход через OIDC не должен давать Client ID Error."
