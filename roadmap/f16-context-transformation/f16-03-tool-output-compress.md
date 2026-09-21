# f16-03. Deterministic compression старых tool results

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-02](./f16-02-context-turns-segments.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Выделить tool-output-compress как самостоятельный Go transformer; recent tail не трогать, принимать и возвращать derived view с source provenance.
- [ ] Добавить консервативные format-aware handlers: ANSI/progress noise, JSON minification без изменения числовых literals, repetitive logs, test output, git/shell output.
- [ ] Сохранять failures/errors, stack traces, exit codes, file:line, commands и значимые последовательности; неизвестный формат оставлять raw. Любой handler применять только при уменьшении размера.
- [ ] Dedup идентичных file reads делать по content/arguments, а не одному filename; ссылка на результат допустима только если его содержимое присутствует в итоговом working set. Без этого оставить raw; diff отложить.
- [ ] Повторно считать tokens после стадии и избегать LLM compaction, если достигнут target budget.

## Критерий готовности

- [ ] Fixtures проверяют сохранность ошибок/anchors и tool protocol, deterministic/idempotent output, отсутствие изменения recent/source и реальные token savings на noisy examples.
- [ ] Числа JSON не округляются; различающиеся file reads не сливаются; неизвестные/небезопасные форматы остаются без изменения.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/](./../../packages/llm-gateway/)
- [packages/llm-gateway/bifrost_executor_test.go](./../../packages/llm-gateway/bifrost_executor_test.go)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
