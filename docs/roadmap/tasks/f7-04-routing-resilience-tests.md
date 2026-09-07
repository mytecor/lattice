# Проверить routing, отказоустойчивость и streaming

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md). Зависит от
[f7-02](./f7-02-declarative-gateway-service.md), [f7-03](./f7-03-logical-model-contract.md) и
реализации runtime с прямым cutover в [f7-07](./f7-07-bifrost-go-proxy.md).

## Контекст

Retry, cooldown, fallback, priority groups, `race`, `hedged` requests и streaming являются
обязательным поведением, а не списком настроек без проверки.

## Что сделать

- [x] Создать fake upstreams с управляемыми задержками, 429, 5xx, обрывами и SSE.
- [x] Проверить каждую routing policy и отмену проигравших parallel requests.
- [x] Проверить, что fallback не нарушает model class и credential boundary.
- [x] Добавить минимальные health/metrics/logs без содержимого prompts и secrets.

## Критерий готовности

- [x] Автоматическая матрица тестов подтверждает все обязательные режимы F7.
- [x] Диагностика показывает выбранный logical route и причину fallback без утечки credentials.

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

**Выполнена 2026-09-07.** После cutover на собственный Go proxy ([f7-07](./f7-07-bifrost-go-proxy.md))
Go-тесты пакета `llm-gateway` покрывают обязательные режимы F7 и закрывают ранее открытые пункты:

- наблюдаемая отмена проигравшего запроса — `TestRaceCancelsLoser`;
- обрыв уже начатого SSE — `TestSelectedStreamHonorsClientCancellation`,
  `TestStreamingRaceIgnoresErrorBeforeWinner`;
- первый meaningful streaming event — `TestStreamingWinnerRequiresMeaningfulEvent`,
  `TestBifrostExecutorStreamingMeaningfulChunk`;
- cooldown, retry, fallback, priority, race, hedge, stage-timeout — `TestCooldownSkipsRecentlyFailedProvider`,
  `TestRetryWrapsPreviousRace`, `TestFallbackRunsOnlyAfterMatchingFailure`, `TestHedgeDelaysSecondProvider`,
  `TestRaceStartsAllProvidersAndFirstSuccessWins`, `TestStageTimeoutIsClassified`;
- отдельный health surface — `GET /healthz` в `server.go` без раскрытия topology;
- structured diagnostic logging — `logging.go`/`logging_test.go`: `route_stage`/`route_attempt`/
  `request_id` с bounded detail без prompt/secret values (`safeLogDetail`);
- выделенный класс ошибок 404 (`ErrorNotFound`) с включением в retryable set — `bifrost_executor.go`,
  `TestBifrostErrorClassification`.

Поведенческая матрица (429, 503, transport disconnect, Chat и Responses SSE, same-upstream retry,
serial fallback, cooldown, priority, race, hedge, cancellation) после cutover живёт в Go-тестах
пакета `llm-gateway` (см. выше) и в `tests/llm-gateway-bifrost.nix`, а не в удалённом в f7-08
`tests/token-proxy-spike.py`. `go test ./...` и все gateway evaluation checks (bifrost, service)
зелёные.
