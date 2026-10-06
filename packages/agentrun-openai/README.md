# agentrun-openai

OpenAI-совместимый HTTP-шлюз над
[`github.com/dmora/agentrun`](https://github.com/dmora/agentrun)
(форк [`github.com/mytecor/agentrun`](https://github.com/mytecor/agentrun)).
Экспонирует агентские CLI (Claude Code, Codex ACP, Antigravity CLI) как модели
OpenAI API, держа их процессы, инструменты и сессии живыми внутри agentrun.

Источник: [`github.com/mytecor/agentrun-openai`](https://github.com/mytecor/agentrun-openai), пин на commit `c23d957` ветки `main` (3 коммита после последнего релиза `v0.2.0`).

С `c23d957` effort-варианты моделей авто-обнаруживаются из agentrun model-catalog
(флаг `--effort-format` удалён), а в шлюзе появилась client function calling
через session-scoped MCP-мост.

## Сборка

Пакет собирает один бинарь `agentrun-openai` (`./cmd/agentrun-openai`).
Версия stampится `-ldflags -X main.version=0.2.0-unstable-2026-10-07` (как `scripts/build-release.sh`).

## Интеграция

- NixOS-модуль: [`modules/agentrun-openai`](../../modules/agentrun-openai/README.md) — service.
- Профиль ноды: [`profiles/agentrun-openai`](../../profiles/agentrun-openai/README.md).
- `c23d957` пин: rev `c23d957` / `c23d957b79e28244836895be3866f45e85091d83` (`hash` и `vendorHash` зафиксированы в `package.nix`).

Сессии agent CLI аутентифицируются в самом процессе по `HOME`-каталогам
пользователя сервиса; модуль не трогает их содержимое.
