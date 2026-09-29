# F12. Observability (метрики gateway + Grafana)

Наблюдаемость LLM gateway и ноды в целом: низко-cardinality метрики, структурированные JSON
события и дашборды. Цель — делать декларативные правки маршрутизации (балансировка, retry,
fallback, cooldown) на основе **измеряемых данных** (latency, TTFT, tokens, error rate), а не
ручных live-прогонов.

**Статус:** ✅ выполнена 2026-09-16 — [f12-01](./f12-01-gateway-metrics-endpoint.md) и
[f12-02](./f12-02-gateway-structured-events.md) реализованы 2026-09-15 (`/metrics` на чистом
stdlib + отдельный loopback-листенер; структурированные JSON-события request/attempt с usage,
TTFT и переходами retry/fallback/hedge); [f12-03](./f12-03-observability-stack.md) реализована
2026-09-16 (модули Prometheus/Loki/Alloy/Grafana, профиль, секреты, тест);
[f12-04](./f12-04-grafana-dashboards.md) реализована 2026-09-16 (дашборды «LLM Gateway»,
«Gateway runtime», «Loki / Расследование» + `environment`-лейбл и контракт-тест).

Два независимых пути сбора, один frontend:

```text
llm-gateway
 ├─ /metrics ───────> Prometheus ──> Grafana   (числовые метрики: RPS, latency, tokens, errors)
 └─ stdout JSON ────> Alloy ──> Loki ──> Grafana (расследование запросов по request_id)
```

Соответствует [вехе 12](../../ROADMAP.md#f12-observability-метрики-gateway--grafana).

Задачи: [f12-01](./f12-01-gateway-metrics-endpoint.md),
[f12-02](./f12-02-gateway-structured-events.md),
[f12-03](./f12-03-observability-stack.md),
[f12-04](./f12-04-grafana-dashboards.md),
[f12-05](./f12-05-dashboard-polish.md) (доработка дашбордов: status, p50/p99, data links, версия сборки).

## Follow-up: наблюдаемость ноды

- [f12-06](./f12-06-node-system-metrics.md) — 🟡 Go `node-status`, системные метрики,
  стабильность systemd-сервисов и `Node overview`: реализация готова 2026-09-29, live-приёмка
  после deploy ещё не выполнена.
- [f12-07](./f12-07-node-alerting.md) — P0: Prometheus alerts и notification route для service
  down/restart burst/resource exhaustion.
- [f12-08](./f12-08-storage-health.md) — P1: SMART/NVMe, Btrfs integrity/scrub и ресурс
  накопителей через отдельную privilege boundary.
- [f12-09](./f12-09-operational-probes.md) — P2: synthetic probes, deploy/backup freshness,
  time/network health и позднее `r1sd`/`containerd`.

Базовая F12 остаётся завершённой; follow-up не блокируют F10/F11. Порядок продолжения:
live-приёмка f12-06 → alerts f12-07 → storage health f12-08 → probes f12-09.

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
