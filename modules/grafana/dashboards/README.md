# Grafana dashboards (f12-04)

Dashboard definitions live here as Grafana dashboard JSON so the repository is
the single source of truth (provisioning, not hand-editing in the UI). The
module's Grafana provisioning points the `lattice` provider at this directory,
so every file here becomes a dashboard under the "Lattice" folder at startup.

Fleet (rev. 2026-09-16, полная переработка):

- [`llm-gateway.json`](./llm-gateway.json) — «LLM Gateway»: обзор. Верхний ряд
  KPI отвечает «всё ли в порядке» за ~3 секунды: RPS, доля ошибок, p95
  длительности, p95 TTFT, активные запросы, число нездоровых провайдеров.
  Ниже: трафик по route/status, задержки p50/p95/p99, надёжность (attempts,
  fallback, in-flight, `llm_balance_health`), токены; логи по `request_id` —
  внизу. Переменные `environment`, `route`, `provider`, `model`, `status`,
  `request_id`.
- [`gateway-runtime.json`](./gateway-runtime.json) — «Gateway runtime»: сервис
  (версия `llm_gateway_build_info`, аптайм, горутины, куча Go), балансировка
  (выборы `llm_balance_selections_total`, здоровье пула `llm_balance_health`),
  события пула из Loki (cooldown/retry/fallback/hedge/semaphore + счёт за
  период), диагностика (динамика горутин/кучи, сбои `catalog_refresh_failed`).
- [`loki-investigation.json`](./loki-investigation.json) — «Loki /
  Расследование»: точка входа без ввода id — таблица «Последние запросы»
  (`request_completed|request_failed`, JSON-поля извлекаются трансформацией
  `extractFields`, клик по `request_id` фильтрует панели); счёт событий по
  типам; лента сбоев и переходов; полная лента одного запроса по переменной
  `request_id` (хронологический порядок).

### Ревизия 2026-09-16 (переработка UX)

Принцип: верх дашборда отвечает «всё ли в порядке» за ~3 секунды, логи — внизу
(раньше дашборды открывались пустой полноэкранной лог-панелью).

- `llm-gateway`: добавлен KPI-ряд (RPS, доля ошибок с порогами 5%/15%, p95
  длительности и TTFT, in-flight, нездоровые провайдеры), ряды «Трафик»,
  «Задержки» (p50/p95/p99), «Надёжность и пул провайдеров» (включая
  `llm_balance_health` таймсерией), «Токены»; Loki-логи перенесены вниз.
  Аннотация «Ошибки запросов» (`event="request_failed"`) рисует красные метки
  на графиках.
- `gateway-runtime`: исправлен мёртвый фильтр Loki-панелей — события это JSON-строки,
  селектор обязан идти после `| json` (раньше `| event=~"..."` без парсера
  матчен пустоту); добавлены heap/goroutines, счёт переходов за период
  (`count_over_time`), версия сборки и аптайм.
- `loki-investigation`: таблица «Последние запросы» наверху (browsable список
  завершённых запросов без ввода id, трансформация `extractFields`, unit `ms`
  для `duration_ms`/`ttft_ms`, клик по `request_id` фильтрует расследование);
  счёт событий по типу; полная лента запроса — внизу, по возрастанию времени.

## Используемые метрики (источник — `/metrics` gateway, f12-01)

| Метрика | Что показывает |
| --- | --- |
| `llm_requests_total{route,model,provider,status}` | завершённые client-запросы (status = success/failed) |
| `llm_request_duration_seconds_bucket{route,model}` | гистограмма длительности (p50/p95/p99) |
| `llm_ttft_seconds_bucket{model}` | гистограмма TTFT (p50/p95/p99) |
| `llm_input_tokens_total` / `llm_output_tokens_total{model}` | накопленные токены |
| `llm_attempts_total{provider,error_type}` | попытки веток по провайдеру и классу ошибки (errors фильтрует `error_type=~".+"`) |
| `llm_fallbacks_total{from_provider,to_provider,reason}` | явные fallback-переходы |
| `llm_balance_health{provider}` | здоровье пула [0..1] |
| `llm_balance_selections_total{route,provider}` | кого выбрала balance-действие |
| `llm_requests_in_flight{provider}` | ветки в полёте по провайдеру |
| `llm_gateway_build_info{version,service}` | версия сборки gateway |
| `process_start_time_seconds` / `go_goroutines` / `go_memstats_alloc_bytes` | runtime-состояние сервиса |

## Loki-события (источник — [f12-02](../../../roadmap/f12-observability/f12-02-gateway-structured-events.md))

События идут в Loki как JSON-строки stream `service="llm-gateway"`; поля
извлекаются парсером `| json` (обязательно перед фильтрами по полям —
селектор без парсера молча матчит пустоту). Типы `event`: `request_received`,
`request_completed`, `request_failed`, `upstream_request_accepted`,
`upstream_request_failed`, `upstream_request_cancelled`, `llm_attempt`,
`llm_retry`, `llm_fallback`, `hedge_launched`, `semaphore_denied`,
`llm_stream_break`, `provider_request_rejected`, `cooldown_put`,
`catalog_refresh_failed`.

## Димензии

Все дашборды фильтруются по `environment` (постоянный label scrape job,
см. `modules/observability-prometheus`), `route`, `provider`, `model`, `status`.
`request_id` — единственная текстовая переменная, для Loki-запросов только;
никогда не label (F12 низкая cardinality). Loki-поток не имеет `environment`
label, поэтому Loki-панели не фильтруются по среде. Панели используют
`$__rate_interval` (Prometheus) и `$__rate_interval`/`$__range` (Loki).
Фильтр `request_id` — точное равенство (`request_id="${request_id}"`): пустая
переменная оставляет панель пустой вместо вывода всего потока.
