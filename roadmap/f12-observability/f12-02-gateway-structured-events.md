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

- [ ] 1. **`event` + единый формат.** Каждая строка — один JSON-объект с `ts`, `level`,
      `event`, `service: "llm-gateway"` и димензиями. Не `logfmt`, не human-readable. Сохранить
      `slog` как провайдера structured JSON (`logging.go` уже JSON), добавить поле `event` с
      устойчивыми именами.
- [ ] 2. **Request-level события** (обычный успешный запрос = минимум строк, одна
      `request_completed`):

      ```json
      {"ts":"...","level":"info","event":"request_completed","service":"llm-gateway",
       "request_id":"req_01K...","route":"standard","provider":"gonka","model":"DeepSeek-V4-Flash-0731",
       "status":"success","status_code":200,"duration_ms":1843,"ttft_ms":312,
       "input_tokens":12430,"output_tokens":821,"cached_tokens":8192,"stream":true,"attempts":3,"winner":"gonka"}
      ```

      Плюс `request_received` при старте (как сейчас `request started`) и `request_failed` при
      ошибке. **Не логировать prompt/body/headers/response body/API keys** (существующее
      ограничение безопасности сохраняется).
- [ ] 3. **Attempt-level события** — отдельные строки только когда происходит что-то интересное:
      retry, race, fallback, timeout, hedge:

      ```json
      {"ts":"...","level":"warn","event":"llm_attempt","request_id":"req_01K...","attempt_id":"...",
       "route":"standard","provider":"provider-a","model":"DeepSeek-V4-Flash-0731",
       "attempt":1,"status":"failed","status_code":429,"error_type":"rate_limit","duration_ms":522}
      ```

      Второй attempt (success):

      ```json
      {"ts":"...","level":"info","event":"llm_attempt","request_id":"req_01K...","attempt_id":"...",
       "route":"standard","provider":"provider-b","model":"DeepSeek-V4-Flash-0731",
       "attempt":2,"status":"success","duration_ms":614}
      ```

      События `fallback`, `retry`, `race`, `hedge_launched`, `semaphore_denied`, `cooldown_put`
      появляются только когда сработали. Уровень: retry/fallback/race-без-успеха — `warn`,
      обычный success — `info`, диагностика перехода — `debug`.
- [ ] 4. **usage** — парсинг `usage` из ответа/стрим-чанков (уже есть в слое
      `bifrost_executor.go` через `schemas`): `input_tokens`, `output_tokens`, `cached_tokens`;
      в стриме — счёт из `usage` финального чанка или сумма по delta-полям.
- [ ] 5. **TTFT** — из первого meaningful chunk (`probeStream`: `res.started` → `res.finished`);
      для non-stream — время ответа.
- [ ] 6. **Тесты**: событие `request_completed` содержит все поля и `ttft_ms`/tokens;
      `llm_attempt` появляется ровно при retry/fallback; безопасность: в событиях нет
      prompt/keys; `-race` чисто.

## Критерий готовности (Definition of Done)

- [ ] 1. Для обычного запроса в журнале одна `request_completed`-строка со `status_code`,
      `duration_ms`, `ttft_ms`, `input_tokens`, `output_tokens`, `cached_tokens`, `stream`;
      для fail-запроса — `request_failed` + attempt-события по каждому сработавшему переходу.
- [ ] 2. По `request_id` в журнале и в графе можно восстановить путь: какие attempt-строки
      принадлежат одному `request_id`, где произошёл retry/fallback/race.
- [ ] 3. В ни одной строке журнала нет prompt/body/headers/API keys; low-cardinality димензии
      как в [f12-01](./f12-01-gateway-metrics-endpoint.md).

## Затрагиваемые файлы / слои

- `packages/llm-gateway/server.go`, `router.go`, `scheduler.go`, `bifrost_executor.go`,
  `logging.go` (формат + `event`), `packages/llm-gateway/README.md`, `modules/llm-gateway/README.md`.

## Открытые вопросы

- Нужен ли отдельный `attempt_id` (uuid per attempt) или достаточно `(request_id, attempt)` через
  уже существующий `route_attempt` в `logRequestAttrs`. По умолчанию — `attempt` + optional
  `attempt_id`.
