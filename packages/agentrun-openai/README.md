# agentrun-openai

OpenAI-совместимый HTTP-шлюз над
[`github.com/dmora/agentrun`](https://github.com/dmora/agentrun)
(форк [`github.com/mytecor/agentrun`](https://github.com/mytecor/agentrun)).
Экспонирует агентские CLI (Claude Code, Codex ACP, Antigravity CLI) как модели
OpenAI API, держа их процессы, инструменты и сессии живыми внутри agentrun.

Источник: [`github.com/mytecor/agentrun-openai`](https://github.com/mytecor/agentrun-openai), пин на release `v0.1.1`.

## Сборка

Пакет собирает один бинарь `agentrun-openai` (`./cmd/agentrun-openai`).
Версия stampится `-ldflags -X main.version=v0.1.1` (как `scripts/build-release.sh`).

## Интеграция

- NixOS-модуль: [`modules/agentrun-openai`](../../modules/agentrun-openai/README.md) — service.
- Профиль ноды: [`profiles/agentrun-openai`](../../profiles/agentrun-openai/README.md).
- `v0.1.1` пин: rev `7c8de8f` (`hash` и `vendorHash` зафиксированы в `package.nix`).

Сессии agent CLI аутентифицируются в самом процессе по `HOME`-каталогам
пользователя сервиса; модуль не трогает их содержимое.
