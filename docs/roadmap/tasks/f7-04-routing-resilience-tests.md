# Проверить routing, отказоустойчивость и streaming

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md). Зависит от
[f7-02](./f7-02-declarative-gateway-service.md), [f7-03](./f7-03-logical-model-contract.md) и
выбора runtime с прямым cutover в [f7-06](./f7-06-go-lip-gonka-cutover.md).

## Контекст

Retry, cooldown, fallback, priority groups, `race`, `hedged` requests и streaming являются
обязательным поведением, а не списком настроек без проверки.

## Что сделать

- [x] Создать fake upstreams с управляемыми задержками, 429, 5xx, обрывами и SSE.
- [ ] Проверить каждую routing policy и отмену проигравших parallel requests.
- [x] Проверить, что fallback не нарушает model class и credential boundary.
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

## Статус

Матрица `tests/token-proxy-spike.py` уже воспроизводит 429, 503, transport disconnect, Chat и
Responses SSE, same-upstream retry, serial fallback, cooldown, priority, race и hedged dispatch.
Она проверяет сохранение logical model/credential boundary и SQLite diagnostics без prompt и
secret values. Четыре последовательных полных прогона прошли 2026-09-05.

Для завершения f7-04 остаются наблюдаемая отмена проигравшего запроса (не только быстрый возврат
победителя), обрыв уже начатого SSE, отдельный health surface и утверждённый минимальный набор
метрик/полей журналирования. Эти проверки не подменяются наличием настроек в runtime.
