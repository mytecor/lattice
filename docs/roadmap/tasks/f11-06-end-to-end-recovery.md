# Провести end-to-end recovery drill controller/worker

Фича: [F11 — controller](../features/f11-controller.md). Зависит от f11-02–f11-05.

## Контекст

Финальный критерий проверяет весь путь от enqueue до result при сбоях worker и controller.

## Что сделать

- [ ] Выполнить обычный путь `enqueue → provision → Pi RPC → publish → destroy`.
- [ ] Повторить с потерей worker до heartbeat expiry и после внешнего side effect.
- [ ] Перезапустить controller с восстановлением из backup/control-plane storage.
- [ ] Сверить task state, единственность result, artifact digests, отсутствие orphan workers и cleanup.

## Критерий готовности

- [ ] Во всех сценариях task либо завершается один раз, либо остаётся в явном actionable state.
- [ ] Потеря worker/cache не требует ручного восстановления данных; восстановление controller state
  следует проверенному runbook.

## Затрагиваемые файлы / слои

- end-to-end/fault-injection tests
- backup/recovery runbook
- roadmap status

## Открытые вопросы

_нет_.
