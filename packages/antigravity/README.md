# Antigravity CLI

[Пакет](./package.nix) упаковывает официальный бинарный дистрибутив **Google Antigravity CLI** (`agy` / `antigravity`) версии `1.3.5`.

CLI представляет собой автономный терминальный агентный харнесс (TUI), поддерживающий работу с моделями семейства Gemini, локальное выполнение команд, планирование задач, MCP-серверы и интеграцию с платформой Antigravity.

## Бинарники и псевдонимы

Пакет устанавливает в `$out/bin`:
- `antigravity` — основной бинарный файл CLI.
- `agy` — символическая ссылка на `antigravity`.

## Платформы

Поддерживаются платформы `x86_64-linux` и `aarch64-darwin`. На Linux зависимости динамической линковки разрешаются через `autoPatchelfHook` и `stdenv.cc.cc.lib`.

## Связанные компоненты

- ACP-сервер: [`packages/antigravity-acp`](./../antigravity-acp/README.md)
- OpenAI-совместимый шлюз: [`packages/agentrun-openai`](./../agentrun-openai/README.md)
