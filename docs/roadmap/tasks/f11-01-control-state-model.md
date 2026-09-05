# Определить модель control-plane state

Фича: [F11 — controller](../features/f11-controller.md). Зависит от F10.

## Контекст

Controller storage является source of truth только для queue, leases, task state и worker
registry. Source/tasks/migrations остаются в Git, artifacts — в object storage.

## Что сделать

- [ ] Описать versioned state machine задачи и допустимые переходы.
- [ ] Определить сущности task, attempt, lease, worker, result и artifact reference.
- [ ] Разделить authoritative state и производные logs/metrics/cache.
- [ ] Выбрать storage после проверки consistency, backup и migration требований.

## Критерий готовности

- [ ] Schema запрещает неоднозначные terminal states и потерю связи result с task attempt.
- [ ] Для каждого поля указан source of truth, retention и recovery path.

## Затрагиваемые файлы / слои

- controller schema/migrations
- `ARCHITECTURE.md`
- `docs/roadmap/BACKLOG.md`

## Открытые вопросы

Конкретный controller storage — часть открытого решения №4 в [BACKLOG.md](../BACKLOG.md).
