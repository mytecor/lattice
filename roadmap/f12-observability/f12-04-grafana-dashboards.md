# f12-04. Grafana-дашборды для LLM gateway

Фича: [F12 — Observability](./README.md). Зависит от
[f12-03](./f12-03-observability-stack.md) (стек) и [f12-01](./f12-01-gateway-metrics-endpoint.md) /
[f12-02](./f12-02-gateway-structured-events.md) (метрики/события).

## Контекст

Стек [f12-03](./f12-03-observability-stack.md) доставляет метрики и события; нужны
дашборды, на которых видно решения настройки балансировки/retry/fallback с первого взгляда —
без чтения journald.

## Что сделать

- [ ] 1. **Дашборд «LLM Gateway» (димензия route/provider/model)**: RPS (`llm_requests_total`),
      latency p50/p95 (`llm_request_duration_seconds_bucket`), TTFT (`llm_ttft_seconds_bucket`),
      tokens input/output (`llm_input_tokens_total` / `llm_output_tokens_total`), errors по
      `error_type` (`llm_attempts_total`), `llm_requests_in_flight{provider}`,
      `llm_balance_health{provider}` (пул «здоров/исключён»), `llm_fallbacks_total{
      from_provider,to_provider,reason}`.
- [ ] 2. **Дашборд «Gateway runtime»**: состояние сервисов (`systemd`-метрики, если экспортер
      есть, либо uptime), catalog refresh failures, cooldown-состояния, панель «кто сейчас
      выбран round_robin по route» (`llm_balance_selections_total`).
- [ ] 3. **Дашборд «Loki / Расследование»**: шаблонная переменная `request_id`, панель-логи по
      `request_id` (из `f12-02`), подсветка retry/fallback/race, время ответа и outcome.
- [ ] 4. **Dashboard JSON как провижниг**: класть dashboard definitions в репозиторий
      (`modules/grafana/`), провайдинг через Grafana provisioning API/файлы (источник истины —
      репозиторий, не ручная правка в UI).
- [ ] 5. **Переменные**: `route`, `provider`, `model`, `environment`, `status`. Использовать
      `$__rate_interval`, `min step` в панелях.
- [ ] 6. **Проверка**: после нескольких запросов в дашборде видно распределение по провайдерам,
      p95 latency, TTFT; скриншот (или описательная проверка) — в задачу.

## Критерий готовности (Definition of Done)

- [ ] 1. Дашборд с: RPS, latency p50/p95, TTFT, tokens, error distribution по provider, health
      pool, fallback counts — виден в Grafana на ноде.
- [ ] 2. Dashboard JSON находится в репозитории и грузится provisioning-ом, а не создаётся
      вручную в UI.
- [ ] 3. По шаблонной переменной `request_id` открывается связка события `request_completed` +
      attempt-события (ретраи/фолбэки) без чтения journald.

## Затрагиваемые файлы / слои

- `modules/grafana/` (provisioning, dashboard JSON), дашборды в `modules/grafana/dashboards/`,
  `nodes/mytecor-homelab/config.nix`, README (`modules/grafana/README.md` или
  [f12-03](./f12-03-observability-stack.md)).

## Открытые вопросы

- Нужны ли отдельные дашборды на один файл или одна панель-логи; можно начать с одного
  «LLM Gateway» с вкладками и добавить второй позже.
