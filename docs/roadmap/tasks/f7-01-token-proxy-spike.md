# Проверить `mxyhi/token_proxy` executable spike

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md).

## Контекст

`mxyhi/token_proxy` — основной кандидат, но архитектура не должна зависеть от неподтверждённых
свойств проекта. До полноценного модуля нужен воспроизводимый spike на закреплённой версии.

## Что сделать

- [x] Закрепить revision и способ сборки headless runtime на NixOS.
- [x] Поднять два управляемых OpenAI-compatible upstream и проверить Chat/Responses API, SSE,
  `race` и `hedged` dispatch.
- [x] Проверить client authentication, замену upstream credentials и поведение `/v1/models`.
- [x] Сопоставить результат со всеми требованиями F7 и зафиксировать выбор или причины замены.

## Критерий готовности

- [x] Spike воспроизводится одной задокументированной командой из репозитория.
- [x] По каждому обязательному свойству есть наблюдаемый результат, а выбор gateway закрыт в
  `BACKLOG.md`.

## Результат

Выполнено 2026-09-05. Выбран `mxyhi/token_proxy` `v0.1.175`, commit
`afba1cc6c0386219b6b519f3b4fc9c2a83198d34`. Source и Cargo dependency graph закреплены в
корневом `flake.lock`; `packages/token-proxy` собирает только headless CLI.

Воспроизводимая команда на NixOS:

```console
nix build .#checks.x86_64-linux.token-proxy-spike -L
```

Spike поднимает управляемые OpenAI-compatible upstream и наблюдаемо проверяет:

- отказ клиенту без gateway credential и замену client credential отдельными upstream keys;
- Chat Completions и Responses API в streaming SSE;
- отображение logical model в provider-specific ID только на upstream boundary и обратную
  подмену model в ответе;
- `/v1/models`, содержащий только `cheap`, `standard`, `strong`, `frontier` без upstream IDs и
  реальных моделей;
- повтор запроса на том же upstream, serial fallback после 5xx, пропуск cooled-down upstream и
  выбор более высокого priority group;
- одновременный запуск кандидатов и возврат быстрого upstream без ожидания медленного для `race`,
  а также отложенный параллельный запуск для `hedged`.

В закреплённом upstream обнаружены два дефекта headless-пути: конкурентная первичная
инициализация SQLite и сохранение старого `Content-Length` после model rewrite. Пакет применяет
два минимальных локальных patch-файла; integration spike является регрессией для обоих. До
удаления patch нужно подтвердить эквивалентное исправление в новой закреплённой версии.

Spike также наблюдаемо подтвердил priority groups, same-upstream retry, 429/503/transport fallback,
cooldown и отсутствие prompt/credential values в штатных SQLite diagnostics. Полная проверка
обрыва начатого SSE, отмены проигравших запросов и operational health surface относится к
[f7-04](./f7-04-routing-resilience-tests.md), поэтому завершение spike не означает завершение всей
F7.

## Затрагиваемые файлы / слои

- `flake.nix`, `flake.lock`
- `checks/` или flake checks
- `docs/roadmap/BACKLOG.md`

## Открытые вопросы

Окончательный выбор gateway — открытое решение №2 в [BACKLOG.md](../BACKLOG.md).
