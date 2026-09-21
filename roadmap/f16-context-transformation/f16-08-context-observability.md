# f16-08. Метрики transformation и стоимость summarization

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-06](./f16-06-context-routing-integration.md), [F12](./../f12-observability/README.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Добавить before/after estimated tokens, compression ratio, stage durations, cache hits/misses, internal summary tokens/calls, bypass/failure/overflow counters.
- [ ] Отделить estimated input от provider-reported usage и внутреннюю стоимость от клиентского usage; request completion учитывать ровно один раз.
- [ ] Связать preparation и main attempts через request_id в structured events, без prompt/summary/tool text, hashes, credentials и user identifiers.
- [ ] Расширить существующий gateway dashboard: warm/cold latency, savings против summary cost, errors и fallback; labels только из ограниченных наборов.

## Критерий готовности

- [ ] Тесты проверяют точный lifecycle счётчиков на success/bypass/failure/cancellation и отсутствие чувствительного content/high-cardinality labels.
- [ ] Один запрос прослеживается от preparation до main completion; cold/warm costs различимы, внутренние ошибки не портят main-provider health.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/metrics.go](./../../packages/llm-gateway/metrics.go)
- [packages/llm-gateway/logging.go](./../../packages/llm-gateway/logging.go)
- [packages/llm-gateway/usage.go](./../../packages/llm-gateway/usage.go)
- [modules/grafana/dashboards/llm-gateway.json](./../../modules/grafana/dashboards/llm-gateway.json)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
