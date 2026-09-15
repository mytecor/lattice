# f12-02. Структурированные JSON-события в llm-gateway

Фича: [F12 — Observability](./README.md). Пакет:
[`packages/llm-gateway`](../../packages/llm-gateway/README.md).

## Контекст

Текущие логи gateway — это `request started/completed/failed` и `upstream accepted/failed/
cancelled` (structured, но без granularity): нельзя по одному `request_id` восстановить переход
retry → fallback → race, увидеть usage или TTFT. Плюс у нас нет `usage`, `ttft_ms`,
`cached_tokens`. Для расследования конкретных запросов и для будущих дашбордов нужна **одна
строка = один JSON event**, одна строка на запрос и отдельные события на интересные переходы.

Определяем событийную иерархию (согласована с [f12-01](./f12-01-gateway-metrics-endpoint.md)):

```text
request
└── attempt
    ├── provider A
    ├── provider B
    └── provider C
```

Логи остаются событийными (расследование по `request_id`), а не источником RPS/latency — это
делают [метрики](./f12-01-gateway-metrics-endpoint.md).

## Что сделать

- [x] 1. **`event` + единый формат.** Каждая строка — один JSON-объект с `time` (slog),
      `level`, `event`, `service: "llm-gateway"` и димензиями. Не `logfmt`, не human-readable.
      Сохранён `slog` как провайдер structured JSON (`logging.go` уже JSON), добавлено поле
      `event` через `logEvent` с устойчивыми именами и low-cardinality routing-контекстом.
- [x] 2. **Request-level события** (обычный успешный запрос = минимум строк, одна
      `request_completed`):

      ```json
      {"time":"...","level":"INFO","msg":"request_completed","event":"request_completed","service":"llm-gateway",
       "request_id":"...","route":"standard","logical_model":"standard","provider":"a","kind":"chat",
       "status":"success","status_code":200,"duration_ms":1843,"ttft_ms":312,
       "input_tokens":12430,"output_tokens":821,"cached_tokens":8192,"stream":false,"attempts":3}
      ```

      Плюс `request_received` при старте (заменяет `request started`), `request_failed` при
      ошибке и `request_rejected` на ранних отклонениях (invalid body/json/model).
      **Не логируется prompt/body/headers/response body/API keys** (существующее
      ограничение безопасности сохранено и покрыто тестом).
- [x] 3. **Attempt-level события** — отдельные строки только когда происходит что-то интересное:
      retry, fallback, hedge:

      ```json
      {"time":"...","level":"WARN","msg":"llm_retry","event":"llm_retry","service":"llm-gateway",
       "request_id":"...","route":"standard","provider":"a","error_type":"429",
       "status_code":429,"attempt":1}
      ```

      Успешная ретрай-попытка пишет `llm_attempt` (success).

      ```json
      {"time":"...","level":"INFO","msg":"llm_attempt","event":"llm_attempt","service":"llm-gateway",
       "request_id":"...","route":"standard.retry","provider":"b","status":"success","attempt":1}
      ```

      События `llm_retry`, `llm_fallback`, `hedge_launched`, `semaphore_denied`, `cooldown_put`
      появляются только когда сработали. Уровень: retry/fallback/cooldown — `warn`,
      обычный success — `info`, hedge/semaphore — `debug`.
- [x] 4. **usage** — расширенный парсинг `usage` (`usage.go` → `extractUsageFull` с
      `cached_tokens`); в стриме — сумма по usage-чанкам победителя.
- [x] 5. **TTFT** — из первого meaningful chunk (`probeStream`: `res.started` → `res.finished`),
      прокинут в `RunOutcome.TTFT` и `SelectedStream.TTFT`;
      для non-stream — полное время ответа.
      для non-stream — время ответа.
- [ ] 6. **Тесты**: событие `request_completed` содержит все поля и `ttft_ms`/tokens;
      `llm_attempt` появляется ровно при retry/fallback; безопасность: в событиях нет
      prompt/keys; `-race` чисто.

## Критерий готовности (Definition of Done)

- [x] 1. Для обычного запроса в журнале одна `request_completed`-строка со `status_code`,
      `duration_ms`, `ttft_ms`, `input_tokens`, `output_tokens`, `cached_tokens`, `stream`,
      `provider`, `route`, `attempts` (проверено тестом `TestRequestCompletedEventCarriesFullFields`);
      для fail-запроса — `request_failed` (+ `llm_retry`/`llm_fallback` на переходах, проверено
      тестами `TestRequestFailedEventOnTimeout`, `TestRetryEmitsAttemptEvents`,
      `TestFallbackEmitsFallbackEvent`).
- [x] 2. По `request_id` в журнале и в графе можно восстановить путь: все строки одного запроса
      несут общий `request_id` (через `logRequestAttrs`), переходы llm_retry/llm_fallback/hedge
      связываются тем же id; числовая сторона даёт тот же путь через метрики f12-01.
- [x] 3. В ни одной строке журнала нет prompt/body/headers/API keys (покрыто
      `TestEventsNeverContainSecrets` — проверяет отсутствие API key, credentials и prompt-текста
      при всех уровнях); low-cardinality димензии
      как в [f12-01](./f12-01-gateway-metrics-endpoint.md) (`event`, `service`, `route`,
      `provider`, `logical_model`, `status`, `error_type`; `request_id` только в поле).

## Реализация 2026-09-15

Событийный слой построен поверх существующего slog JSON: `logEvent(ctx, logger, level, event,
attrs...)` в `logging.go` подставляет `service` и `event` в каждую строку и наследует
`request_id` через `logRequestAttrs`. Логгер прокинут в `Runner` (поле `logger`), так что
переходы (retry/fallback/cooldown/semaphore/hedge) пишут события из scheduler/router, а
request-level события — из `server.go`. `usage.go` расширен до `extractUsageFull`
(`cached_tokens`); TTFT прокинут в `RunOutcome.TTFT`/`SelectedStream.TTFT`; счётчик попыток — в
`routeRuntime.attempts` → `outcome.attempts`. Тесты: `events_test.go` (`-race` чисто).

`go test -race ./...` и `go vet ./...` зелёные; `nix flake check --all-systems --no-build` —
см. проверку ниже в CI. Бинарь пересобран и smoke-проверен локально.

## Затрагиваемые файлы / слои

- `packages/llm-gateway/server.go`, `router.go`, `scheduler.go`, `router_stream.go`,
  `logging.go` (формат + `event`), `usage.go`, `events_test.go` (новый),
  `packages/llm-gateway/README.md`, `modules/llm-gateway/README.md`.

## Открытые вопросы

- `attempt_id` (uuid per attempt) не добавляем: связывание по `(request_id, attempt)` достаточно
  и компактнее, `attempt` уже есть в событиях (счётчик попыток графа).
