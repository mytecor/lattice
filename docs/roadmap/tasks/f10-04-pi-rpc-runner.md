# Запускать task через Pi RPC

Фича: [F10 — disposable worker](../features/f10-disposable-worker.md). Зависит от f8-05 и f10-03.

## Контекст

Worker не получает отдельный agent harness: тот же Pi runtime, который используется в TUI,
запускается через RPC с task specification и чистым workspace.

## Что сделать

- [ ] Преобразовать validated task specification во вход Pi RPC без скрытой session state.
- [ ] Потоково собирать events/logs и различать result, tool failure, model failure и timeout.
- [ ] Передавать только logical model class и разрешённый tool profile.
- [ ] Добавить deterministic smoke task с fake gateway и real Git workspace.

## Критерий готовности

- [ ] Pi RPC завершает smoke task и возвращает машинно-читаемый result envelope.
- [ ] Повтор на новом worker не требует данных предыдущей Pi session.

## Затрагиваемые файлы / слои

- worker runner
- `profiles/pi/`
- integration checks

## Открытые вопросы

_нет_.
