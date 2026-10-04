{ lib, buildGoModule, fetchFromGitHub }:
# agentrun-openai — OpenAI-совместимый HTTP-шлюз над agentrun
# (github.com/dmora/agentrun). Предоставляет persistent Claude Code / Codex /
# Antigravity CLI-сессии как модели OpenAI API: библиотека agentrun живёт в
# процессе gateway и сама запускает agent CLI, хранит их сессии и транслирует
# в чат-историю. Пакет пинится на релиз-тег v0.1.1 (rev 7c8de8f) внешнего
# репозитория.
#
# go.mod использует replace-директиву на форк github.com/mytecor/agentrun
# (v0.9.1-0.20260829100251-c3c1411b7ff1); форк не имеет внешних зависимостей,
# поэтому go mod vendor в общем модуле включает его целиком, и vendorHash ниже
# покрывает всё дерево зависимостей (см. packages/foxbridge/package.nix).
buildGoModule rec {
  pname = "agentrun-openai";
  version = "0.1.1";

  src = fetchFromGitHub {
    owner = "mytecor";
    repo = "agentrun-openai";
    rev = "v0.1.1";
    hash = "sha256-RNbosYVDPqDARV2NpxLEm8ug9ujDnxMsqhjFMkjFqhQ=";
  };

  # Версия stamp через -ldflags, как делает scripts/build-release.sh.
  ldflags = [
    "-s"
    "-w"
    "-X main.version=v0.1.1"
  ];

  # Computed from the pinned source's go.mod (github.com/dmora/agentrun with
  # replace to github.com/mytecor/agentrun): `go mod vendor`, then
  # `nix hash path --type sha256 vendor` (2026-10-05).
  vendorHash = "sha256-R9ms2yhDtqHqjj3BCz+z2ES5i+otjPko4wluX6qKXzY=";

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
