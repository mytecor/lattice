# agentrun-openai

OpenAI-совместимый HTTP-шлюз над
[`github.com/dmora/agentrun`](https://github.com/dmora/agentrun)
(форк [`github.com/mytecor/agentrun`](https://github.com/mytecor/agentrun)).
Экспонирует агентские CLI (Claude Code, Codex ACP, Antigravity CLI) как модели
OpenAI API, держа их процессы, инструменты и сессии живыми внутри agentrun.

Источник: [`github.com/mytecor/agentrun-openai`](https://github.com/mytecor/agentrun-openai), пин на commit `e11419e` ветки `main` (4 коммита после последнего релиза `v0.2.0`).

С `c23d957` effort-варианты моделей авто-обнаруживаются из agentrun model-catalog
(флаг `--effort-format` удалён), а в шлюзе появилась client function calling
через session-scoped MCP-мост; с `e11419e` завершённые tool-сессии также
восстанавливаются через ACP `session/load` со свежим MCP-мостом после
idle eviction и рестарта шлюза.

## Сборка

Пакет собирает один бинарь `agentrun-openai` (`./cmd/agentrun-openai`).
Версия stampится `-ldflags -X main.version=0.2.0-unstable-2026-10-08` (как `scripts/build-release.sh`).

## Интеграция

- NixOS-модуль: [`modules/agentrun-openai`](../../modules/agentrun-openai/README.md) — service.
- Профиль ноды: [`profiles/agentrun-openai`](../../profiles/agentrun-openai/README.md).
- `e11419e` пин: rev `e11419e` / `e11419e4d0d2fc34d26fce563a8fafee8869ac94` (`hash` и `vendorHash` зафиксированы в [`package.nix`](./package.nix)).

Сессии agent CLI аутентифицируются в самом процессе по `HOME`-каталогам
пользователя сервиса; модуль не трогает их содержимое.
