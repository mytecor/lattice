#!/usr/bin/env bash
# f4-04: сгенерировать /run/lattice-node-status.json на каждой активации из
# runtime-фактов узла (NixOS generation, применённый comin source revision).
#
# Файл пишется транзакционно (временный файл + mv), чтобы Caddy никогда не
# отдал частично записанный JSON. При недоступности источника commit поле
# остаётся null; от активации это не должно зависнуть.
set -euo pipefail

: "${LATTICE_NODE_STATUS_FILE:?LATTICE_NODE_STATUS_FILE is required}"
: "${LATTICE_NODE_STATE_VERSION:?LATTICE_NODE_STATE_VERSION is required}"
: "${LATTICE_COMIN_SOURCE_REPO:?LATTICE_COMIN_SOURCE_REPO is required}"

# Пути по умолчанию можно переопределить для тестов/контейнеров.
current_system_link="${LATTICE_CURRENT_SYSTEM_LINK:-/run/current-system}"
node="${LATTICE_NODE_NAME:-$(hostname)}"

# NixOS generation: /run/current-system -> /nix/var/nix/profiles/system-N-link.
# Берём непосредственную цель (readlink без -f), т.к. сама system-N-link в свою
# очередь указывает на /nix/store/...-nixos-system-..., и basename финальной цели
# уже не содержит номера поколения.
current_system="$(readlink "$current_system_link" 2>/dev/null || true)"
generation="null"
if [[ -n "$current_system" ]]; then
  gen="$(basename "$current_system")"           # system-123-link
  if [[ "$gen" =~ ^system-([0-9]+)(-link)?$ ]]; then
    generation="${BASH_REMATCH[1]}"
  fi
fi

# Применённый commit: наш comin-source-sync пишет refs/lattice/source как
# фактически выбранный head исходника (radicle/origin) до нормализации.
# Это тот же смысл, что generation.source.git.main_commit_id у comin, но без
# зависимости от внутреннего формата comin store.json.
commit="null"
source_ref="refs/lattice/source"
if [[ -d "$LATTICE_COMIN_SOURCE_REPO" ]]; then
  ref=$(git -C "$LATTICE_COMIN_SOURCE_REPO" rev-parse --verify --quiet "$source_ref" 2>/dev/null || true)
  if [[ -n "$ref" && "$ref" =~ ^[0-9a-f]{40}$ ]]; then
    commit="\"$ref\""
  fi
fi

kernel="$(uname -r 2>/dev/null || true)"
activated_at="$(date +%s)"

# Транзакционная запись. jq собирает валидный JSON и экранирует строки.
tmp="${LATTICE_NODE_STATUS_FILE}.tmp.$$"
jq -n \
  --arg node "$node" \
  --arg service "lattice-node-status" \
  --argjson generation "$generation" \
  --argjson commit "$commit" \
  --arg kernel "$kernel" \
  --arg stateVersion "$LATTICE_NODE_STATE_VERSION" \
  --argjson activatedAt "$activated_at" \
  '{ node: $node, service: $service, generation: $generation, commit: $commit,
     kernel: $kernel, stateVersion: $stateVersion, activatedAt: $activatedAt }' \
  > "$tmp"
chmod 0644 "$tmp"
mv "$tmp" "$LATTICE_NODE_STATUS_FILE"
