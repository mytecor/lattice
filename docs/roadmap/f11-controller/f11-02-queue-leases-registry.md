# Реализовать очередь, leases и worker registry

Фича: [F11 — controller](./README.md). Зависит от f11-01.

## Контекст

Controller должен назначать задачу одному активному attempt, замечать потерянный worker и
возвращать незавершённую работу в очередь без ручного редактирования state.

## Что сделать

- [ ] Реализовать enqueue/claim/renew/complete/fail transitions с atomic lease ownership.
- [ ] Вести worker registration, capabilities, heartbeat и drain state.
- [ ] Обрабатывать expiry lease и stale worker без потери task specification.
- [ ] Добавить concurrency tests для competing workers и clock/timeout boundaries.

## Критерий готовности

- [ ] Одну lease одновременно владеет не более одного worker attempt.
- [ ] Потерянный worker обнаруживается, а task безопасно становится доступной для нового attempt.

## Затрагиваемые файлы / слои

- controller service
- controller schema/migrations
- concurrency tests

## Открытые вопросы

_нет_.
