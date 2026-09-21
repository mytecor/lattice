#!/usr/bin/env bash
#
# get-authentik-creds.sh — вывести учётные данные для входа на SSO-портал
# auth.mytecor-homelab.local (Authentik): логин, email, пароль и bootstrap-токен.
#
# Источник — agenix-секреты в nodes/mytecor-homelab/secrets/*.age (в Git только
# шифротекст). Каждый .age расшифровывается приватным age-identity ноды
# .secrets/mytecor-homelab.agekey (НЕ в Git) и содержит одну строку формата
# AUTHENTIK_*=... (EnvironmentFile).
#
# Часть [F14 — SSO через Authentik](../roadmap/f14-sso-authentik/README.md):
# bootstrap-операторская учётка создаётся Authentik при первом старте из
# AUTHENTIK_BOOTSTRAP_USERNAME/_EMAIL/_PASSWORD; это и есть логин на
# auth.mytecor-homelab.local.
#
# Запуск (с рабочего каталога = корень репозитория):
#   ./scripts/get-authentik-creds.sh
#
# Переопределение путей через переменные окружения:
#   AGE_KEY   приватный age-identity (default .secrets/mytecor-homelab.agekey)
#   SECRETS   каталог с *.age        (default nodes/mytecor-homelab/secrets)
#
# Замечание по выводу: скрипт печатает значения в stdout — это операторский
# скрипт, оператор его сам запускает. Значения намеренно не попадают в командную
# строку/history (расшифровка идёт pipe'ом, не через аргументы).

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
AGE_KEY=${AGE_KEY:-"$REPO_ROOT/.secrets/mytecor-homelab.agekey"}
SECRETS=${SECRETS:-"$REPO_ROOT/nodes/mytecor-homelab/secrets"}

command -v age >/dev/null || { echo "required command missing: age" >&2; exit 1; }
[[ -f "$AGE_KEY" ]] || { echo "age identity missing: $AGE_KEY" >&2; exit 1; }
[[ -d "$SECRETS" ]] || { echo "secrets dir missing: $SECRETS" >&2; exit 1; }

# decrypt_var VARNAME <file.age> — печатает значение переменной VARNAME из секрета.
# Значение идёт в stdout только через подстановку $(...), в историю не попадает.
decrypt_var() {
  local var=$1 file=$2
  age -d -i "$AGE_KEY" "$file" | sed -n 's/^[[:space:]]*'"$var"'[[:space:]]*=[[:space:]]*//p'
}

USERNAME=$(decrypt_var AUTHENTIK_BOOTSTRAP_USERNAME "$SECRETS/authentik-bootstrap-user.age")
EMAIL=$(decrypt_var    AUTHENTIK_BOOTSTRAP_EMAIL    "$SECRETS/authentik-bootstrap-email.age")
PASSWORD=$(decrypt_var AUTHENTIK_BOOTSTRAP_PASSWORD "$SECRETS/authentik-bootstrap-password.age")
TOKEN=$(decrypt_var    AUTHENTIK_BOOTSTRAP_TOKEN    "$SECRETS/authentik-bootstrap-token.age")

for v in USERNAME EMAIL PASSWORD TOKEN; do
  [[ -n "${!v}" ]] || { echo "empty value for $v — secret расшифрован некорректно" >&2; exit 1; }
done

cat <<EOF
SSO portal : https://auth.mytecor-homelab.local

username   : $USERNAME
email      : $EMAIL
password   : $PASSWORD
bootstrap token: $TOKEN
EOF
