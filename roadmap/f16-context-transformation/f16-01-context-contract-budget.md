# f16-01. Контракты pipeline и token budget

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: существующая F7. Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Определить независимые Go-интерфейсы трёх stages, immutable SourceHistory, source ranges и типизированные outcomes: passthrough/transformed/unsupported/overflow/error.
- [ ] Добавить versioned token accounting: messages, tools/schema, response format, multimodal overhead; exact estimator либо conservative estimate с margin. Не использовать usage предыдущего ответа как размер нового запроса.
- [ ] Определить capabilities всех достижимых provider/native targets, включая fallback/hedge/continue, и бюджет с учётом output reserve, tokenizers и strip/set params. Catalog IDs не выдавать за context metadata.
- [ ] Реализовать policy soft/target/hard и отдельный tail budget согласно design; отсутствующий limit требует override. Проверять текущий request/output limits до дорогостоящей работы.
- [ ] Зафиксировать поведение disabled, unsupported content, неизвестной оценки и oversized mandatory context; preserve неизвестных JSON-полей.

## Критерий готовности

- [ ] Табличные тесты покрывают пороги, overhead tools, разные tokenizers/windows, output aliases/defaults и меньший fallback; готовый budget безопасен для каждого target.
- [ ] Ниже soft threshold body возвращается byte-identical и summarizer не вызывается; oversized mandatory context получает типизированную ошибку.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/config.go](./../../packages/llm-gateway/config.go)
- [packages/llm-gateway/router.go](./../../packages/llm-gateway/router.go)
- [packages/llm-gateway/catalog.go](./../../packages/llm-gateway/catalog.go)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
