# f16-06. Подключение pipeline перед routing

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-03](./f16-03-tool-output-compress.md), [f16-04](./f16-04-history-compact-cache.md), [f16-05](./f16-05-context-select.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Подключить один orchestration hook в Server.inference после auth/model validation до RunWithResult/SelectStream; сохранить immutable source и prepared request.
- [ ] Использовать одинаковый prepared body в retry/fallback/race/hedge без повторной summarization; model rewrite и strip/set остаются в Bifrost executor, не должны обходить budget checks.
- [ ] Оставить Responses API/affinity без transformation. Проверить chat adapters, unknown JSON fields, stream и non-stream.
- [ ] Внутренние LLM calls пометить internal без публичного bypass-header; не заходить через HTTP facade. Учитывать budgets и access scope независимо от main attempts.
- [ ] Для continue использовать prepared working set + accumulated partial; recount перед takeover, отдельный reserve. Не пересуммаризировать и не удалять выданный partial; overflow отдавать штатным terminal SSE error.
- [ ] Провести cancellation/deadline через все stages; fail-open только при проверенном hard budget, typed errors без утечки content/native IDs.

## Критерий готовности

- [ ] Fake executor видит один prepared body на всех обычных attempts, source не мутируется; cancellation останавливает summarizer и не начинает main upstream call.
- [ ] Интеграционные тесты покрывают stream/non-stream, retry/race/hedge/fallback, continue с partial и overflow, Responses bypass и отсутствие recursive summary calls.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/server.go](./../../packages/llm-gateway/server.go)
- [packages/llm-gateway/router.go](./../../packages/llm-gateway/router.go)
- [packages/llm-gateway/router_stream.go](./../../packages/llm-gateway/router_stream.go)
- [packages/llm-gateway/continue.go](./../../packages/llm-gateway/continue.go)
- [packages/llm-gateway/bifrost_executor.go](./../../packages/llm-gateway/bifrost_executor.go)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
