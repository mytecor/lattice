# Подключить provisioner disposable workers

Фича: [F11 — controller](./README.md). Зависит от
[f11-02](./f11-02-queue-leases-registry.md) и
[F10](../f10-disposable-worker/README.md).

## Контекст

Проверенный вручную lifecycle F10 должен запускаться через узкий versioned provisioner interface,
не встраивая конкретный container backend в scheduler. Первой implementation остаётся
`LocalExecutor → containerd`; после готовности r1s добавляется `R1SExecutor`, который переводит тот
же workload contract в allocation protocol.

## Что сделать

- [ ] Определить provisioner operations create/status/terminate и idempotency keys.
- [ ] Реализовать adapter для выбранного в F10 isolation backend.
- [ ] Сохранить `LocalExecutor` как single-node adapter и добавить отдельный `R1SExecutor`, не
      перенося discovery/offers/assignment в общий provisioner contract.
- [ ] Связать worker identity и lease без передачи controller storage credentials.
- [ ] Обработать partial create, unreachable worker и repeated terminate.

## Критерий готовности

- [ ] Controller создаёт и уничтожает worker через versioned interface.
- [ ] Повтор команды после неизвестного результата не создаёт orphan или второй активный worker.

## Затрагиваемые файлы / слои

- controller provisioner interface/adapter
- worker bootstrap
- integration tests

## Открытые вопросы

Controller storage остаётся частью открытого решения №4 в [BACKLOG.md](../BACKLOG.md); граница
provisioner и первая local implementation уже зафиксированы.
