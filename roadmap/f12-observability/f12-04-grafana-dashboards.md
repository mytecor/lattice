# f12-04. Grafana-дашборды для LLM gateway

Фича: [F12 — Observability](./README.md). Зависит от
[f12-03](./f12-03-observability-stack.md) (стек) и [f12-01](./f12-01-gateway-metrics-endpoint.md) /
[f12-02](./f12-02-gateway-structured-events.md) (метрики/события).

## Контекст

Стек [f12-03](./f12-03-observability-stack.md) доставляет метрики и события; нужны
дашборды, на которых видно решения настройки балансировки/retry/fallback с первого взгляда —
без чтения journald.

## Что сделать

- [x] 1. **Дашборд «LLM Gateway» (димензия route/provider/model)**: RPS (`llm_requests_total`),
      latency p50/p95 (`llm_request_duration_seconds_bucket`), TTFT (`llm_ttft_seconds_bucket`),
      tokens input/output (`llm_input_tokens_total` / `llm_output_tokens_total`), errors по
      `error_type` (`llm_attempts_total`), `llm_requests_in_flight{provider}`,
      `llm_balance_health{provider}` (пул «здоров/исключён»), `llm_fallbacks_total{
      from_provider,to_provider,reason}`.
- [x] 2. **Дашборд «Gateway runtime»**: состояние сервисов (uptime через
      `process_start_time_seconds`, `go_goroutines`), catalog refresh failures (Loki: событие
      `catalog_refresh_failed`), cooldown/retry/fallback/hedge-события (Loki), панель «кто сейчас
      выбран round_robin/adaptive по route» (`llm_balance_selections_total`), health пула
      (`llm_balance_health`).
- [x] 3. **Дашборд «Loki / Расследование»**: шаблонная переменная `request_id`, панель-логи по
      `request_id` (из `f12-02`), подсветка retry/fallback/race (счёт по `event` и все строки
      запроса), время ответа и outcome.
- [x] 4. **Dashboard JSON как провижниг**: dashboard definitions в репозитории
      (`modules/grafana/dashboards/`), провайдинг через Grafana provisioning файлы (источник
      истины — репозиторий, не ручная правка в UI); модуль предет дефолтный провайдер,
      `dashboardProviders` может заменить его целиком.
- [x] 5. **Переменные**: `route`, `provider`, `model`, `environment`, `status`. Используются
      `$__rate_interval` / `$__interval`, `min step` не хардкодится.
- [x] 6. **Проверка**: контракт-тест `tests/grafana-dashboards.nix` (валидность JSON, uids,
      surface метрик, `environment`-лейбл скрейп-джоба, текстовые переменные); live-скриншот —
      после деплоя на ноду (шаг деплоя в f12-03/f12-04).

## Критерий готовности (Definition of Done)

- [x] 1. Дашборд с: RPS, latency p50/p95, TTFT, tokens, error distribution по provider, health
      pool, fallback counts — виден в Grafana на ноде. *(Конфигурация и JSON готовы,
      provisioning сборен; live-проверка на ноде — шаг деплоя.)*
- [x] 2. Dashboard JSON находится в репозитории и грузится provisioning-ом, а не создаётся
      вручную в UI. *(Провидер указывает на `modules/grafana/dashboards/`, проверено
      контракт-тестом.)*
- [x] 3. По шаблонной переменной `request_id` открывается связка события `request_completed` +
      attempt-события (ретраи/фолбэки) без чтения journald. *(Панель-логи в «Loki /
      Расследование» и в «LLM Gateway»; live-подтверждение — шаг деплоя.)*

## Затрагиваемые файлы / слои

- `modules/grafana/` (provisioning, dashboard JSON), дашборды в `modules/grafana/dashboards/`
  (`llm-gateway.json`, `gateway-runtime.json`, `loki-investigation.json`), `nodes/mytecor-homelab/config.nix`,
  README (`modules/grafana/README.md`).
- `modules/observability-prometheus/` — постоянный `environment`-лейбл скрейп-джоба
  (низкая cardinality, переменная `environment` в дашбордах).
- `tests/grafana-dashboards.nix` (новый контракт-тест).

## Реализация 2026-09-16

Три дашборда как provisioning-файлы в `modules/grafana/dashboards/` (репозиторий — источник
истины, Grafana file provider подхватывает их в папке "Lattice"):

- **`llm-gateway.json`** — «LLM Gateway»: RPS по route, latency p50/p95 и TTFT p95
  (histogram_quantile), input/output tokens по model, errors по `error_type`
  (`llm_attempts_total`), in-flight по provider, fallbacks по from→to+reason, панель-логи Loki по
  `request_id`. Переменные: `environment`, `route`, `provider`, `model`, `status
  `, `request_id` (textbox).
- **`gateway-runtime.json`** — «Gateway runtime»: uptime (`process_start_time_seconds`) и
  goroutines из runtime `/metrics`, catalog refresh failures (Loki `catalog_refresh_failed`),
  selection rate round_robin/adaptive (`llm_balance_selections_total`), health пула
  (`llm_balance_health`), события cooldown/retry/fallback/hedge (Loki).
- **`loki-investigation.json`** — «Loki / Расследование»: счёт по `event` и полная лента событий
  одного запроса по шаблонной переменной `request_id` — связка request/attempt (`llm_retry`,
  `llm_fallback`, `hedge_launched`) из [f12-02](./f12-02-gateway-structured-events.md).

Поддерживающие изменения: постоянный `environment`-лейбл скрейп-джоба в
`modules/observability-prometheus/` (одно значение на деплой, низкая cardinality; опции
`gatewayEnvironment`); контракт-тест `tests/grafana-dashboards.nix` (валидность JSON, стабильные
uids, димензии и текст функции дашбордов, `$__rate_interval`, `environment`-лейбл,
`request_id` только как текстовая переменная). `nix flake check --no-build` зелёный;
live-проверка на ноде — шаг деплоя.

## Открытые вопросы

- Отдельные дашборды на один файл против одной панели-логов: сделаны два — «LLM Gateway» (число)
  и «Loki / Расследование» (логи по `request_id`); runtime-состояние — отдельным третьим
  «Gateway runtime». Отдельные панели можно добавлять к существующим без новых файлов.
