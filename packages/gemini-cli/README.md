# Gemini CLI

[Пакет](./package.nix) упаковывает официальный дистрибутив **Google Gemini CLI** (`@google/gemini-cli`) версии `0.64.0`, собранный через общий [`packages/pnpm-cli-builder`](./../pnpm-cli-builder/README.md).

Gemini CLI представляет собой терминальный агентный харнесс с поддержкой моделей Gemini, сессионного планирования, встроенных и подключаемых инструментов, расширений и нативной поддержки протокола ACP (Agent Client Protocol).

## Бинарники

Пакет устанавливает в `$out/bin`:
- `gemini` — основной бинарный файл CLI.

## Запуск в ACP-режиме

Gemini CLI поддерживает нативный режим ACP по флагу `--acp`:

```sh
gemini --acp
```

В этом режиме харнесс взаимодействует с клиентом (IDE или шлюзом) через JSON-RPC 2.0 поверх `stdio`.

## Связанные компоненты

- Изолированный ACP-сервер: [`packages/gemini-acp`](./../gemini-acp/README.md)
- OpenAI-совместимый шлюз: [`packages/agentrun-openai`](./../agentrun-openai/README.md)
