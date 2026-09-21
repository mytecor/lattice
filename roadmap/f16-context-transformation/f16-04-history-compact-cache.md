# f16-04. Cached factual summaries и retrieval synopsis

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-02](./f16-02-context-turns-segments.md), [f16-03](./f16-03-tool-output-compress.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Реализовать history-compact: отдельные factual summary, retrieval synopsis, structured facts/exact anchors, provenance и completeness metadata на sealed segment.
- [ ] Добавить внутренний summarizer adapter поверх существующего execution layer с bypass transformation; allowlisted target policy, cancellation, timeout, concurrency и token/attempts/cost limits.
- [ ] Валидировать schema/size, сохранять решения и причины, команды, identifiers и ошибки; prompt трактует историю как данные. Malformed/empty/hallucinated anchors не принимать как подтверждённые факты.
- [ ] Реализовать versioned content-addressed cache, trust namespace, singleflight и atomic first-success write; hit возвращает bytes без повторной генерации.
- [ ] Добавить bounded memory cache и opt-in persistence: quota/TTL, 0700/0600, atomic replacement, corruption recovery, restart reuse. Не сохранять raw prompts по умолчанию.
- [ ] Ввести abstraction store/summarizer внутри Go без внешнего protocol; явная invalidation при смене prompt/model/version и прозрачная политика cache eviction.

## Критерий готовности

- [ ] Counting fake summarizer доказывает один вызов на неизменный key, reuse при append/restart, изоляцию namespaces и singleflight при конкурентных requests.
- [ ] Ошибки/отмена/повреждённая запись не отравляют cache; warm result byte-identical; лимиты внутренних вызовов и storage проверяются независимо от production значений.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/](./../../packages/llm-gateway/)
- [packages/llm-gateway/router.go](./../../packages/llm-gateway/router.go)
- [packages/llm-gateway/bifrost_executor.go](./../../packages/llm-gateway/bifrost_executor.go)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
