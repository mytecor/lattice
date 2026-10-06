{ lib, buildGoModule, fetchFromGitHub }:
# agentrun-openai — OpenAI-совместимый HTTP-шлюз над agentrun
# (github.com/dmora/agentrun). Предоставляет persistent Claude Code / Codex /
# Antigravity CLI-сессии как модели OpenAI API: библиотека agentrun живёт в
# процессе gateway и сама запускает agent CLI, хранит их сессии и транслирует
# в чат-историю. Пакет пинится на commit c23d957 ветки main внешнего
# репозитория (3 коммита после последнего релиз-тега v0.2.0): в числе прочего
# effort-варианты теперь авто-обнаруживаются из model-catalog вместо
# --effort-format, и появилась client function calling через session-scoped
# MCP-мост.
#
# go.mod использует replace-директиву на форк github.com/mytecor/agentrun
# (v0.9.1-0.20261006200631-e07f67cf8aca); форк не имеет внешних зависимостей,
# поэтому go mod vendor в общем модуле включает его целиком, и vendorHash ниже
# покрывает всё дерево зависимостей (см. packages/foxbridge/package.nix).
buildGoModule rec {
  pname = "agentrun-openai";
  # 3 коммита после v0.2.0, пин на коммит (тега нет): c23d957.
  version = "0.2.0-unstable-2026-10-07";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "agentrun-openai";
    rev = "c23d957b79e28244836895be3866f45e85091d83";
    hash = "sha256-0wBgXZCCW4aEu3NZHxemhMmim0WV4FPvaAmlpIs01sk=";
  };

  # Версия stamp через -ldflags, как делает scripts/build-release.sh.
  ldflags = [
    "-s"
    "-w"
    "-X main.version=0.2.0-unstable-2026-10-07"
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
