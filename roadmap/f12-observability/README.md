# F12. Observability (метрики gateway + Grafana)

Наблюдаемость LLM gateway и ноды в целом: низко-cardinality метрики, структурированные JSON
события и дашборды. Цель — делать декларативные правки маршрутизации (балансировка, retry,
fallback, cooldown) на основе **измеряемых данных** (latency, TTFT, tokens, error rate), а не
ручных live-прогонов.

**Статус:** 🚧 в работе — [f12-01](./f12-01-gateway-metrics-endpoint.md) и
[f12-02](./f12-02-gateway-structured-events.md) реализованы 2026-09-15 (`/metrics` на чистом
stdlib + отдельный loopback-листенер; структурированные JSON-события request/attempt с usage,
TTFT и переходами retry/fallback/hedge); f12-03..f12-04 ещё не
начаты.

Два независимых пути сбора, один frontend:

```text
llm-gateway
 ├─ /metrics ───────> Prometheus ──> Grafana   (числовые метрики: RPS, latency, tokens, errors)
 └─ stdout JSON ────> Alloy ──> Loki ──> Grafana (расследование запросов по request_id)
```

Соответствует [вехе 12](../../ROADMAP.md#f12-observability-метрики-gateway-графана).

Задачи: [f12-01](./f12-01-gateway-metrics-endpoint.md),
[f12-02](./f12-02-gateway-structured-events.md),
[f12-03](./f12-03-observability-stack.md),
[f12-04](./f12-04-grafana-dashboards.md).

**Критерий готовности:** любой запрос через gateway наблюдаем двумя путями: числовые метрики
(RPS, latency p50/p95, TTFT, input/output tokens, errors, cost) доступны в Prometheus и видны в
Grafana-дашборде; конкретный запрос связывается по `request_id` от JSON-события до перехода в
retry/fallback/race. Ни одна из димензий логов и метрик не содержит высоко-cardinality полей
(`request_id`, session id, prompt hash, client API key). Grafana и хранилища не публичны и не
раскрывают provider credentials.

**Осознанно откладываем (до F…):** OpenTelemetry traces — только после того, как метрики и логи
покроют реальные вопросы настройки (роль trace в этом графе ограничена: retry/fallback/race уже
связываются через `request_id`).

## Дизайн: метрики против логов, событийная иерархия

Метрики **не строятся из логов как основной механизм**: RPS/latency/tokens считаются в
Prometheus-счётчиках и гистограммах на лету, а логи остаются событийными (расследование одного
запроса). Метрики быстрее, дешевле и корректнее для агрегата.

### Низкая cardinality

Константные лейблы и для Prometheus, и для Loki:

```text
service   environment   route   provider   model   status   error_type
```

Не делать лейблами: `request_id`, `session_id`, `user_id`, `api_key`, `prompt_hash`, `client_ip`.

### Request/attempt

Две сущности, связываемые по `request_id` (и `attempt_id` на уровне попыток):

```text
request
└── attempt
    ├── provider A
    ├── provider B
    └── provider C
```

Обычный успешный запрос пишет минимум событий (одно `request_completed`); retry/race/fallback/
timeout — отдельные событные строки (см. [f12-02](./f12-02-gateway-structured-events.md)).
