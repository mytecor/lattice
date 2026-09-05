# Проверить routing, отказоустойчивость и streaming

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md). Зависит от
[f7-02](./f7-02-declarative-gateway-service.md) и [f7-03](./f7-03-logical-model-contract.md).

## Контекст

Retry, cooldown, fallback, priority groups, `race`, `hedged` requests и streaming являются
обязательным поведением, а не списком настроек без проверки.

## Что сделать

- [ ] Создать fake upstreams с управляемыми задержками, 429, 5xx, обрывами и SSE.
- [ ] Проверить каждую routing policy и отмену проигравших parallel requests.
- [ ] Проверить, что fallback не нарушает model class и credential boundary.
- [ ] Добавить минимальные health/metrics/logs без содержимого prompts и secrets.

## Критерий готовности

- [ ] Автоматическая матрица тестов подтверждает все обязательные режимы F7.
- [ ] Диагностика показывает выбранный logical route и причину fallback без утечки credentials.

## Затрагиваемые файлы / слои

- `checks/`
- `profiles/llm-gateway/`
- документация эксплуатации gateway

## Открытые вопросы

_нет_. Численные timeout/delay значения настраиваются после измерений.
