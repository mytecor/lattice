# f16-05. BM25 retrieval и сборка working set

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-01](./f16-01-context-contract-budget.md), [f16-04](./f16-04-history-compact-cache.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Сформировать bounded query из current user request и recent context; обработать follow-up после tool result и короткие ссылки на прошлую задачу.
- [ ] Индексировать synopsis/exact anchors только pages входной source history; BM25 с deterministic tie-break и хронологической выдачей selected pages.
- [ ] Собрать bounded global facts из cached representations без нового giant summarizer, с provenance, порядком решений и явными конфликтами.
- [ ] Резервировать instructions/current/recent, затем global compact, затем relevant summaries; не отправлять все summaries и не дублировать pages. Recount итогового body для всех target estimators.
- [ ] Определить provider-facing historical-data block с безопасной ролью без повышения privileges, invented tool calls и orphan results. Не поддержанные layouts — bypass/error по policy.
- [ ] Заложить representation levels absent/compact/detailed/raw в модели данных; автоматическую promotion, embeddings и hierarchy оставить после MVP.

## Критерий готовности

- [ ] Фикстура с несколькими независимыми задачами возвращает relevant old page и exact recent tail, no-hit не выбирает всю историю; чужие pages из cache недоступны.
- [ ] Итоговый messages[] проходит protocol validator, mandatory content сохраняется, hard budget соблюдён; tie-break и warm assembly воспроизводимы.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/](./../../packages/llm-gateway/)
- [packages/llm-gateway/bifrost_executor_test.go](./../../packages/llm-gateway/bifrost_executor_test.go)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
