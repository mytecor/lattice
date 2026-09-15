# Grafana dashboards (f12-04)

Dashboard definitions live here as Grafana dashboard JSON so the repository is
the single source of truth (provisioning, not hand-editing in the UI). The
module's Grafana provisioning points the `lattice` provider at this directory,
so every file here becomes a dashboard under the "Lattice" folder at startup.

Fleet delivered by f12-04:

- [`llm-gateway.json`](./llm-gateway.json) — «LLM Gateway»: числовые метрики
  gateway (Prometheus) — RPS по route, latency p50/p95, TTFT p95, input/output
  tokens, связка по `request_id` (Loki). Переменные `environment`, `route`,
  `provider`, `model`, `request_id`.
- [`gateway-runtime.json`](./gateway-runtime.json) — «Gateway runtime»: uptime и
  goroutines (runtime-метрики `/metrics`), catalog refresh failures (Loki),
  здоровье пула `llm_balance_health`, selection rate
  `llm_balance_selections_total` (кто выбран round_robin/adaptive), события
  cooldown/retry/fallback/hedge (Loki).
- [`loki-investigation.json`](./loki-investigation.json) — «Loki / Расследование»:
  все события одного запроса по шаблонной переменной `request_id` — связка
  `request_received` → `request_completed`/`request_failed` с промежуточными
  `llm_retry`/`llm_fallback`/`hedge_launched`.

## Используемые метрики (источник — `/metrics` gateway, f12-01)

| Метрика | Что показывает |
| --- | --- |
| `llm_requests_total{route,model,provider,status}` | завершённые client-запросы |
| `llm_request_duration_seconds_bucket{route,model}` | гистограмма длительности (p50/p95) |
| `llm_ttft_seconds_bucket{model}` | гистограмма TTFT |
| `llm_input_tokens_total` / `llm_output_tokens_total{model}` | накопленные токены |
| `llm_attempts_total{provider,error_type}` | попытки веток по провайдеру и классу ошибки |
| `llm_fallbacks_total{from_provider,to_provider,reason}` | явные fallback-переходы |
| `llm_balance_health{provider}` | здоровье пула [0..1] |
| `llm_balance_selections_total{route,provider}` | кого выбрала balance-действие |
| `llm_requests_in_flight{provider}` | ветки в полёте по провайдеру |
| `process_start_time_seconds` / `go_goroutines` | runtime-состояние сервиса |

## Димензии

Все дашборды фильтруются по `environment` (постоянный label scrape job,
см. `modules/observability-prometheus`), `route`, `provider`, `model`, `status`.
`request_id` — только шаблонная переменная для Loki-запросов, никогда не label
(F12 низкая cardinality). Панели используют `$__rate_interval` и `$__interval`.
