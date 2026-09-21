# f16-02. Canonical turns, protected tail и stable segments

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-01](./f16-01-context-contract-budget.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Парсить messages без потери исходных bytes/blocks; сохранить system/developer, exact identifiers и provenance отдельно от derived representations.
- [ ] Группировать user → assistant tool calls → results → continuation, включая parallel calls и несколько tool rounds; определить handling незавершённых и malformed chains.
- [ ] Выделять recent tail целыми turns по count и token quota; current turn обязателен. Не обрезать oversized atomic turn.
- [ ] Строить deterministic pages от начала source по завершённым turns и task/topic signals; закрытые границы независимы от sliding recent window/query. Открытая или пересекающая tail page остаётся raw.
- [ ] Вычислять versioned canonical source hash до lossy compression; хранить ranges для восстановления исходных pages.

## Критерий готовности

- [ ] Append новых turns не меняет границы/hashes sealed prefix; новый current query не меняет segmentation; mutation инвалидирует затронутые representations.
- [ ] Fixtures с parallel/nested rounds, незавершённой цепочкой и длинным turn сохраняют IDs, порядок и exact recent bytes; source восстанавливается без summaries.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/](./../../packages/llm-gateway/)
- [packages/llm-gateway/server_test.go](./../../packages/llm-gateway/server_test.go)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
