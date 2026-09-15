# Подключить provisioner disposable workers

Фича: [F11 — controller](./README.md). Зависит от
[f11-02](./f11-02-queue-leases-registry.md) и
[F10](../f10-disposable-worker/README.md).

## Контекст

Проверенный вручную lifecycle F10 должен запускаться через узкий versioned provisioner interface,
не встраивая конкретный container backend в scheduler. Execution backend — r1s: `R1SExecutor`
переводит тот же workload contract в allocation protocol, не перенося discovery/offers/assignment
в общий provisioner contract.

## Что сделать

- [ ] Определить provisioner operations create/status/terminate и idempotency keys.
- [ ] Реализовать adapter для выбранного в F10 isolation backend.
- [ ] Реализовать `R1SExecutor` как первую implementation поверх клиента r1s и локального
      `r1sd`-allocator, не перенося discovery/offers/assignment в общий provisioner contract.
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
provisioner и первая implementation (`R1SExecutor` поверх локального `r1sd`) уже зафиксированы.
