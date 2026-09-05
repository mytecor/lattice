# Подключить provisioner disposable workers

Фича: [F11 — controller](../features/f11-controller.md). Зависит от f11-02 и F10.

## Контекст

Проверенный вручную lifecycle F10 должен запускаться через узкий versioned provisioner interface,
не встраивая конкретный VM/container backend в scheduler.

## Что сделать

- [ ] Определить provisioner operations create/status/terminate и idempotency keys.
- [ ] Реализовать adapter для выбранного в F10 isolation backend.
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

Конкретный provisioner — часть открытого решения №4 в [BACKLOG.md](../BACKLOG.md).
