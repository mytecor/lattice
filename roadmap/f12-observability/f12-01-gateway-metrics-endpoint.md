# f12-01. Prometheus `/metrics` endpoint в llm-gateway

Фича: [F12 — Observability](./README.md). Пакет:
[`packages/llm-gateway`](../../packages/llm-gateway/README.md).

## Контекст

На homelab gateway уже логирует события, но **числовых метрик нет вообще**: RPS, latency,
TTFT, input/output tokens, cost, ошибки, состояние балансировки (`ScoreStore`) нигде не
экспортируются. Для декларативной настройки маршрутизации (балансировка, retry, fallback,
cooldown) нужны измеримые per-route / per-provider агрегаты, а не ручные live-прогоны из
journald. Метрики считаются в счётчиках и гистограммах на лету, а не из логов.

Зависит от: структуры request/attempt в роутере (см. [f12-02](./f12-02-gateway-structured-events.md)
для единого источника димензий); счётчики могут появляться вместе с событиями.

## Что сделать

- [ ] 1. **Пакет метрик на чистом stdlib** (без внешних prometheus libs, в репозитории нет
      vendor-зависимостей): `packages/llm-gateway/metrics.go` — набор counters, histograms и
      gauges с лейблами только низкой cardinality. Димензии: `service`, `route`, `provider`,
      `model`, `status`, `error_type`. Не включать `request_id`/`session_id`/`api_key`/
      `client_ip` в лейблы.
- [ ] 2. **HTTP `/metrics`**: новый handler (можно по образцу `GET /healthz` в
      [server.go](../../packages/llm-gateway/server.go)) в Prometheus text exposition format;
      отдельная опция конфига для порта/интерфейса (по умолчанию `127.0.0.1:9209`), чтобы
      метрики не были публичны и не требовали `client_api_key`.
- [ ] 3. **Счётчики и гистограммы:**
      `llm_requests_total{route,model,provider,status=success|failed}`;
      `llm_request_duration_seconds_bucket{route,model,le}` (p50/p95);
      `llm_ttft_seconds_bucket{model,le}`; `llm_input_tokens_total{model}`;
      `llm_output_tokens_total{model}`; `llm_requests_in_flight{provider}` (gauge);
      `llm_attempts_total{provider,result=error_type}`; `llm_fallbacks_total{from_provider,to_provider,reason}`.
      Streaming: обновлять продолжительность и токены этого запроса в момент завершения, а не
      в TTF.
- [ ] 4. **Корректность TTFT и tokens**: TTFT брать из первого meaningful chunk
      (`probeStream` / `res.started`→`res.finished`, уже известно); input/output tokens — из
      `usage` ответа и стрим-чанков (см. парсинг usage в [f12-02](./f12-02-gateway-structured-events.md)).
      Если usage недоступен — считаем 0 и помечаем в метрике/событии, не выдумываем.
- [ ] 5. **Состояние балансировки** (то, что сегодня есть только внутри
      `ScoreStore` в памяти): `llm_balance_health{provider}` и счётчик выбора
      `llm_balance_selections_total{route,provider}`, чтобы видеть пул «здоров/исключён/в
      окне фатальных ошибок» и куда уходит round_robin cursor без ручного чтения кода.
- [ ] 6. **Тесты**: рендер exposition format, отсутствие высоко-cardinality лейблов, histogram
      buckets, обновление `in_flight` при concurrent-запросах, `-race` чисто.

## Критерий готовности (Definition of Done)

- [ ] 1. `GET /metrics` отдаёт валидный Prometheus text format; `PROMETHEUS`-скрейп-чек
      (`curl -s localhost:9209/metrics`) показывает counters/histograms по route/provider.
- [ ] 2. Для логических моделей `stupid`/`standard` после нескольких запросов видно распределение
      по провайдерам (RPS, latency, tokens), а не только суммарный счётчик.
- [ ] 3. В метриках нет ни одного высоко-cardinality лейбла; README package описывает
      `/metrics`, порт и список димензий.

## Затрагиваемые файлы / слои

- `packages/llm-gateway/metrics.go` (новый), `server.go`, `config.go`/`config.nix` (порт),
  `main.go` (mount `/metrics`), `modules/llm-gateway/options.nix` и `config.nix` (опция порта),
  `packages/llm-gateway/README.md`, `modules/llm-gateway/README.md`.

## Открытые вопросы

- Используем ли свой exposition renderer или минимальный vendor `prometheus/client_golang`.
  По умолчанию — чистый stdlib (в репозитории нет vendor-зависимостей); строгий
  `DisallowUnknownFields` конфига и прозрачность сборки важнее.
