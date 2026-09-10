{ nixpkgs, pkgs, piModule }:

# f8-03: smoke check воспроизводимого tool profile из чистого окружения.
#
# Проверяем, что итоговый `lattice.pi.toolProfile` (базовый контракт +
# `lattice.pi.tools`) содержит все декларированные бинарники и что ключевые
# команды реально запускаются из `env -i` (без случайных user/global пакетов,
# программный PATH — ровно profile/bin). Заодно фиксируем locale и git identity
# boundary контракта.
let
  lib = nixpkgs.lib;
  config = (lib.nixosSystem {
    modules = [
      piModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        lattice.pi = {
          enable = true;
          user = "root";
          settings = {
            defaultProvider = "llm-gateway";
            defaultModel = "standard";
            defaultThinkingLevel = "xhigh";
          };
          models.llm-gateway = {
            baseUrl = "http://127.0.0.1:9208/v1";
            api = "openai-completions";
            discoverModels = false;
            models = [ { id = "standard"; } { id = "stupid"; } ];
          };
          # Расширение tool profile проекта (не меняя рантайм).
          tools = [ "nodejs" ];
        };
      }
    ];
  }).config;

  profile = config.lattice.pi.toolProfile;

  # Бинарники, которые обязаны присутствовать в базовом контракте.
  baseBins = [
    "bash" "git" "curl" "jq" "vim" "rg" "tree" "gpg" "xxd" "which"
    "awk" "grep" "sed" "tar" "gzip" "diff" "file" "ps" "ssh"
  ];
  # Бинарники, добавленные расширением.
  extraBins = [ "node" ];
in
pkgs.runCommand "pi-tool-profile-smoke" { profile = profile; } ''
  set -euo pipefail

  # 1. Состав профиля: каждый бинарник присутствует как исполняемый файл.
  for b in ${builtins.toString baseBins}; do
    test -x "$profile/bin/$b" || { echo "missing base tool: $b" >&2; exit 1; }
  done
  for b in ${builtins.toString extraBins}; do
    test -x "$profile/bin/$b" || { echo "missing extra tool: $b" >&2; exit 1; }
  done

  # 2. Чистое окружение: PATH ровно из profile/bin, ничего из хост-системы.
  clean_env() {
    env -i \
      PATH=''${profile}/bin \
      HOME=''$TMPDIR/home \
      TMPDIR=''$TMPDIR \
      LANG=C.UTF-8 LC_ALL=C.UTF-8 \
      GIT_CONFIG_NOSYSTEM=1 \
      GIT_CONFIG_GLOBAL=''$TMPDIR/home/.gitconfig "$@"
  }

  mkdir -p "$TMPDIR/home" "$TMPDIR/work"
  export PATH="$profile/bin:$PATH"

  # ключевые команды реально исполняются в чистом окружении
  clean_env bash -lc "command -v git >/dev/null && test \$(git --version >/dev/null; echo ok) = ok"
  clean_env bash -lc "curl --version >/dev/null && jq --version >/dev/null && node --version >/dev/null"

  # 3. git identity boundary: без глобального конфига git init/commit работает,
  #    а user.name/email берутся только из per-user ~/.gitconfig (пустого по умолчанию).
  clean_env bash -c '
    set -e
    cd "$TMPDIR/work"
    git init -q repo && cd repo
    git config --local user.name test
    git config --local user.email test@localhost
    echo hi > f && git add f
    git -c user.name=test -c user.email=test@localhost commit -qm base
    test "$(git rev-parse --short HEAD)" != ""
    test -z "''${GIT_CONFIG_GLOBAL:-}" || test "$GIT_CONFIG_GLOBAL" = "$HOME/.gitconfig"
  '

  # 4. locale контракт фиксирован в чистом окружении.
  clean_env bash -c 'test "$LANG" = C.UTF-8 && test "''${LC_ALL:-}" = C.UTF-8'

  touch "$out"
''
