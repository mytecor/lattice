{ lib, buildGoModule, fetchFromGitHub }:
# agentrun-openai — OpenAI-совместимый HTTP-шлюз над agentrun
# (github.com/dmora/agentrun). Предоставляет persistent Claude Code / Codex /
# Antigravity CLI-сессии как модели OpenAI API: библиотека agentrun живёт в
# процессе gateway и сама запускает agent CLI, хранит их сессии и транслирует
# в чат-историю. Пакет пинится на commit e11419e ветки main внешнего
# репозитория (4 коммита после последнего релиз-тега v0.2.0): effort-варианты
# авто-обнаруживаются из model-catalog, есть client function calling через
# session-scoped MCP-мост и восстановление завершённых tool-сессий через
# ACP session/load после idle eviction и рестарта шлюза.
#
# go.mod использует replace-директиву на форк github.com/mytecor/agentrun
# (v0.9.1-0.20261006200631-e07f67cf8aca); форк не имеет внешних зависимостей,
# поэтому go mod vendor в общем модуле включает его целиком, и vendorHash ниже
# покрывает всё дерево зависимостей (см. packages/foxbridge/package.nix).
buildGoModule rec {
  pname = "agentrun-openai";
  # 4 коммита после v0.2.0, пин на коммит (тега нет): e11419e.
  version = "0.2.0-unstable-2026-10-08";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "agentrun-openai";
    rev = "e11419e4d0d2fc34d26fce563a8fafee8869ac94";
    hash = "sha256-kXPsbTwt5Vi70yMLMH9Nr1r71dz0gg2MI2SRJRqojFM=";
  };

  # Версия stamp через -ldflags, как делает scripts/build-release.sh.
  ldflags = [
    "-s"
    "-w"
    "-X main.version=0.2.0-unstable-2026-10-08"
  ];

  # Computed from the pinned source's go.mod (github.com/dmora/agentrun with
  # replace to github.com/mytecor/agentrun): `go mod vendor`, then
  # `nix hash path --type sha256 vendor` (2026-10-07).
  vendorHash = "sha256-XTaDT9bosMzuA0cVcEcdRp+7+MTVsKfh5PhhoPQIEEQ=";

  subPackages = [ "./cmd/agentrun-openai" ];

  doCheck = true;

  meta = {
    description = "OpenAI-compatible HTTP gateway for persistent Claude Code and Codex sessions powered by agentrun";
    homepage = "https://github.com/mytecor/agentrun-openai";
    license = lib.licenses.mit;
    mainProgram = "agentrun-openai";
    platforms = lib.platforms.linux;
  };
}
