{ pkgs, statusWriter }:

# f4-04: runtime smoke — status-write.sh генерирует валидный JSON из
# runtime-фактов узла: extraction NixOS generation из symlink, commit из
# refs/lattice/source, пустой commit → null. Исполняется в build sandbox, без
# VM; проверяет сам скрипт, а не Nix-конфигурацию.
pkgs.runCommand "node-status-writer-test" {
  nativeBuildInputs = [ pkgs.git pkgs.jq statusWriter ];
} ''
  export HOME="$TMPDIR/home"
  mkdir -p "$HOME" "$TMPDIR/work"
  git config --global user.name test
  git config --global user.email test@localhost

  # Mock ноды, как на реальной ноде: /run/current-system ->
  # /nix/var/nix/profiles/system-42-link (первый уровень readlink даёт номер
  # поколения), а system-42-link в свою очередь -> store-путь nixos-system.
  mkdir -p "$TMPDIR/fs/nix/var/nix/profiles" "$TMPDIR/fs/run"
  ln -sfn "$TMPDIR/fs/nix/store/nixos-system-mock" \
    "$TMPDIR/fs/nix/var/nix/profiles/system-42-link"
  ln -sfn ../nix/var/nix/profiles/system-42-link \
    "$TMPDIR/fs/run/current-system"

  # Mock comin source repo с выбранным head в refs/lattice/source
  git init --bare "$TMPDIR/source-repo"
  git init "$TMPDIR/work"
  git -C "$TMPDIR/work" commit --allow-empty -m one
  head=$(git -C "$TMPDIR/work" rev-parse HEAD)
  git -C "$TMPDIR/source-repo" fetch "$TMPDIR/work" "+$head:refs/lattice/source"

  run_status() {
    LATTICE_NODE_STATUS_FILE="$1" \
    LATTICE_NODE_STATE_VERSION="26.05" \
    LATTICE_COMIN_SOURCE_REPO="$TMPDIR/source-repo" \
    LATTICE_CURRENT_SYSTEM_LINK="$TMPDIR/fs/run/current-system" \
    LATTICE_NODE_NAME="node-a" \
    lattice-node-status-write
    jq -e . "$1" >/dev/null
  }

  # 1. Обычный узел: generation=42, commit из refs/lattice/source
  run_status "$TMPDIR/status1.json"
  test "$(jq -r .generation "$TMPDIR/status1.json")" = "42"
  test "$(jq -r .commit "$TMPDIR/status1.json")" = "$head"
  test "$(jq -r .node "$TMPDIR/status1.json")" = "node-a"
  test "$(jq -r .service "$TMPDIR/status1.json")" = "lattice-node-status"
  test "$(jq -r .stateVersion "$TMPDIR/status1.json")" = "26.05"
  test "$(jq -r .kernel "$TMPDIR/status1.json")" != ""

  # 2. Свежая нода без выбранного коммита → commit=null, JSON валиден
  mkdir -p "$TMPDIR/empty-source"
  git init --bare "$TMPDIR/empty-source/repository"  # без refs
  LATTICE_NODE_STATUS_FILE="$TMPDIR/status2.json" \
  LATTICE_NODE_STATE_VERSION="26.05" \
  LATTICE_COMIN_SOURCE_REPO="$TMPDIR/empty-source/repository" \
  LATTICE_CURRENT_SYSTEM_LINK="$TMPDIR/fs/run/current-system" \
  LATTICE_NODE_NAME="fresh" \
  lattice-node-status-write
  jq -e '.commit == null and (.generation | type == "number")' \
    "$TMPDIR/status2.json" >/dev/null

  # 3. Нет symlink current-system → generation=null (не падает)
  LATTICE_NODE_STATUS_FILE="$TMPDIR/status3.json" \
  LATTICE_NODE_STATE_VERSION="26.05" \
  LATTICE_COMIN_SOURCE_REPO="$TMPDIR/source-repo" \
  LATTICE_CURRENT_SYSTEM_LINK="$TMPDIR/no-such-link" \
  LATTICE_NODE_NAME="no-gen" \
  lattice-node-status-write
  jq -e '.generation == null' "$TMPDIR/status3.json" >/dev/null

  mkdir "$out"
''
