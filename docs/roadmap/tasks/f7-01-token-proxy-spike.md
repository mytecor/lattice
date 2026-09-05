# Проверить `mxyhi/token_proxy` executable spike

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md).

## Контекст

`mxyhi/token_proxy` — основной кандидат, но архитектура не должна зависеть от неподтверждённых
свойств проекта. До полноценного модуля нужен воспроизводимый spike на закреплённой версии.

## Что сделать

- [ ] Закрепить revision и способ сборки headless runtime на NixOS.
- [ ] Поднять два управляемых OpenAI-compatible upstream и проверить Chat/Responses API, SSE,
  `race` и `hedged` dispatch.
- [ ] Проверить client authentication, замену upstream credentials и поведение `/v1/models`.
- [ ] Сопоставить результат со всеми требованиями F7 и зафиксировать выбор или причины замены.

## Критерий готовности

- [ ] Spike воспроизводится одной задокументированной командой из репозитория.
- [ ] По каждому обязательному свойству есть наблюдаемый результат, а выбор gateway закрыт в
  `BACKLOG.md`.

## Затрагиваемые файлы / слои

- `flake.nix`, `flake.lock`
- `checks/` или flake checks
- `docs/roadmap/BACKLOG.md`

## Открытые вопросы

Окончательный выбор gateway — открытое решение №2 в [BACKLOG.md](../BACKLOG.md).
