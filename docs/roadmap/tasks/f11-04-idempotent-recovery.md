# Сделать выполнение идемпотентным и восстанавливаемым

Фича: [F11 — controller](../features/f11-controller.md). Зависит от f11-02 и f11-03.

## Контекст

Сеть и процессы могут падать после фактического side effect, но до записи подтверждения. Retry не
должен дважды публиковать commit/result или терять уже созданный artifact.

## Что сделать

- [ ] Назначить idempotency key каждому attempt и внешнему side effect.
- [ ] Определить reconciliation для неизвестного исхода clone/push/upload/complete.
- [ ] Реализовать bounded retry, backoff, cancellation и dead-letter/manual-attention state.
- [ ] Добавить fault-injection tests на каждой границе до/после side effect.

## Критерий готовности

- [ ] Повтор после injected crash приводит к одному логическому result и целостному state.
- [ ] Неавтоматически разрешимый конфликт становится явным состоянием, а не бесконечным retry.

## Затрагиваемые файлы / слои

- controller reconciliation/retry logic
- fault-injection tests
- operations runbook

## Открытые вопросы

_нет_.
