# Grafana dashboards (f12-04)

Dashboard definitions live here as Grafana dashboard JSON so the repository is
the single source of truth (provisioning, not hand-editing in the UI). Each
service has a subdirectory here; the module's Grafana provisioning declares one
file-type provider per service, so every JSON file becomes a dashboard in the
service's own Grafana folder (f12-10-03: the "Lattice" catch-all folder is
gone — Grafana here is only for Lattice, so folders are per service).

Structure: [`llm-gateway/`](./llm-gateway/) holds the four gateway dashboards,
[`node/`](./node/) the node overview. Subdirectory name == provider name;
Grafana folder is the display name set in [config.nix](../config.nix) (`LLM
Gateway`, `Node`).

Fleet (rev. 2026-09-16, полная переработка):

- [`llm-gateway/llm-gateway.json`](./llm-gateway/llm-gateway.json) — «LLM Gateway»: обзор. Верхний ряд
  KPI отвечает «всё ли в порядке» за ~3 секунды: RPS, доля ошибок, p95
  длительности, p95 TTFT, активные запросы, число нездоровых провайдеров.
  Ниже: трафик по route/status, fallback-переходы, in-flight по провайдеру,
  потребление по клиентским ключам; логи по `request_id` — внизу. Детальный
  разрез по провайдерам/нативным моделям (задержки p50/p95/p99, попытки,
  кулдаун, токены по модели) живёт в `gateway-providers`, здесь он не
  дублируется. Переменные `environment`, `route`, `provider`, `native_model`,
  `status`, `request_id`.
- [`llm-gateway/gateway-providers.json`](./llm-gateway/gateway-providers.json) — «Gateway: модели по
  провайдерам» (f12-06): динамический разрез `native_model`. **Один общий
  набор панелей** (без повторяющейся строки `repeat: provider`) — каждая
  панель показывает всех выбранных провайдеров вместе: RPS, latency/TTFT
  p50/p95/p99, ошибочные попытки, здоровье пула, кулдаун, токены вход/выход
  и средняя длина ответа по нативным моделям. Единственный дашборд с
  провайдерским/модельным срезом деталей (f12-10-03: обзор больше не
  дублирует эти панели). Панели фильтруются `provider=~"$provider"` (regex по
  мультивыбору) и группируются `by (provider, ...)`, так что серии одного
  провайдера никогда не смешиваются с другим; легенды содержат `{{provider}}`.
- [`llm-gateway/gateway-runtime.json`](./llm-gateway/gateway-runtime.json) — «Gateway runtime»: сервис
  (версия `llm_gateway_build_info`, аптайм, горутины, куча Go), балансировка
  (выборы `llm_balance_selections_total`, здоровье пула `llm_balance_health`),
  события пула из Loki (cooldown/retry/fallback/hedge/semaphore + счёт за
  период), диагностика (динамика горутин/кучи, сбои `catalog_refresh_failed`).
- [`llm-gateway/loki-investigation.json`](./llm-gateway/loki-investigation.json) — «Loki /
  Расследование»: точка входа без ввода id — таблица «Последние запросы»
  (`request_completed|request_failed`, JSON-поля извлекаются трансформацией
  `extractFields`, клик по `request_id` фильтрует панели); счёт событий по
  типам; лента сбоев и переходов; полная лента одного запроса по переменной
  `request_id` (хронологический порядок).
- [`node/node-overview.json`](./node/node-overview.json) — «Node overview»: CPU, load, RAM, root filesystem,
  uptime, disk/network throughput, температуры, ошибки сборщика и сети, состояния выбранных
  systemd-сервисов и прирост их рестартов. Переменные `environment`, `unit`, `device`, `disk`.

### Ревизия 2026-09-16 (переработка UX)

Принцип: верх дашборда отвечает «всё ли в порядке» за ~3 секунды, логи — внизу
(раньше дашборды открывались пустой полноэкранной лог-панелью).

- `llm-gateway`: добавлен KPI-ряд (RPS, доля ошибок с порогами 5%/15%, p95
  длительности и TTFT, in-flight, нездоровые провайдеры), ряды «Трафик»,
  «Задержки» (p50/p95/p99), «Надёжность и пул провайдеров» (включая
  `llm_balance_health` таймсерией и панель «Остаток кулдауна по провайдеру»,
  `llm_cooldown_until_seconds − time()`: cooldown теперь виден как 15-секундные
  пики, а не как залипший на нуле health), «Кулдаун провайдеров», «Токены»;
  Loki-логи перенесены вниз.
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

### Ревизия 2026-09-16 (f12-06: димензия `native_model`)

Настоящий id модели провайдера (`native_model`) добавлен в каждую метрику и
событие gateway (f12-06), а дашборды перешли с логической `model` (одинаковой
у всех провайдеров — она скрывала, кто именно отвечал) на `native_model`.

- `llm-gateway`: панель «Распределение результатов запросов» переписана с
  `sum by (status)` на `sum by (provider, native_model, status)` (RPS по тому,
  кто ответил и каким результатом); задержки/TTFT/токены/попытки/кулдаун
  сгруппированы `by (provider, native_model)`; переменная `model` заменена на
  `native_model` (`label_values(llm_requests_total, native_model)`).
- `gateway-providers`: **новый** дашборд — динамический разрез по нативным
  моделям каждого провайдера (повторяющаяся строка `repeat: provider`), см. выше.
- `loki-investigation`: столбец `model` в трансформации `organize` переименован
  в `native_model` («модель (натив.)») — события теперь несут `native_model`.

### Ревизия 2026-10-03 (общий дашборд вместо строки на провайдера)

`gateway-providers` переведён с повторяющейся строки `repeat: provider` (каждый
провайдер — свой набор панелей) на **один общий набор** панелей с той же
раскладкой (RPS / latency / TTFT / попытки / здоровье пула / кулдаун / токены
в тех же gridPos): каждая панель показывает всех выбранных провайдеров вместе.
Панели фильтруются `provider=~"$provider"` (regex по мультивыбору `$provider`)
вместо точного `provider="$provider"`, и каждая сгруппирована/разбита так,
что серии не смешиваются: `sum by (provider, ...)` для RPS/латентности/TTFT/
попыток/токенов, а health/кулдаун несут `provider`-лейбл нативно; все легенды
содержат `{{provider}}`. Контракт-тест обновлён: вместо «есть row `repeat:
provider`» проверяется отсутствие row-repeat и `provider=~"$provider"`.

### Ревизия 2026-10-03 (дедупликация: обзор против провайдерского среза)

`llm-gateway` (обзор) и `gateway-providers` (разрез по провайдерам/моделям)
вместе показывали одни и те же 7 панелей (RPS/латентность/TTFT/попытки/
здоровье пула/кулдаун/токены по модели) — разрез нативного среза жил в
обоих. Убрано с обзора: «Распределение результатов запросов», «Длительность
ответа по провайдеру и модели», «TTFT по провайдеру и модели», «Попытки по
провайдеру, модели и типу ошибки», «Здоровье провайдеров пула», «Остаток
кулдауна по паре провайдер/модель», «Токены в секунду по провайдеру и
модели» — всё это теперь единственно в `gateway-providers`; опустевшие ряды
«Задержки»/«Кулдаун провайдеров»/«Токены» удалены, панель «Средняя длина
ответа» (разрез по провайдеру/модели, раньше была на обзоре) перенесена в
`gateway-providers`. Обзор оставляет: KPI-ряд, трафик по route/status,
fallback, in-flight по провайдеру, потребление по клиентским ключам и логи по
`request_id`. Контракт-тест обновлён: детальный срез теперь проверяется на
`gateway-providers`, а на обзоре запрещены `llm_attempts_total`,
`llm_cooldown_until_seconds` и `histogram_quantile(0.50,` (провайдерский
разрез не должен возвращаться в обзор).

### Ревизия 2026-10-03 (папки по сервисам)

Дашборды разложены по подпапкам одного сервиса — `llm-gateway/` (обзор,
провайдерский срез, runtime, расследование) и `node/` (node overview) —
вместо плоского каталога. Grafana-провид provision объявляет по одному
file-провайдеру на сервис (`name` = имя подпапки), каждый со своей `folder`
(`LLM Gateway`, `Node`); бессмысленная общая папка `Lattice` удалена —
Grafana здесь только под Lattice, так что папки теперь отражают сервисы.
Контракт-тест проверяет: каждый провайдер смотрит в свою подпапку
`dashboards/<service>` и несёт display-`folder`.

## Используемые метрики (источник — `/metrics` gateway, f12-01)

| Метрика | Что показывает |
| --- | --- |
| `llm_requests_total{api_key,route,model,provider,native_model,status}` | завершённые client-запросы (status = success/failed); `api_key` — не-секретный id клиентского ключа (пусто в keyless), `model` — логический, `native_model` — реальный id у провайдера |
| `llm_request_duration_seconds_bucket{api_key,route,model,provider,native_model}` | гистограмма длительности (p50/p95/p99) |
| `llm_ttft_seconds_bucket{model,provider,native_model}` | гистограмма TTFT (p50/p95/p99) |
| `llm_input_tokens_total` / `llm_output_tokens_total{api_key,model,provider,native_model}` | накопленные токены (по клиентскому ключу) |
| `llm_attempts_total{provider,native_model,error_type}` | попытки веток по провайдеру/модели и классу ошибки (errors фильтрует `error_type=~".+"`) |
| `llm_fallbacks_total{from_provider,to_provider,reason}` | явные fallback-переходы |
| `llm_balance_health{provider}` | скользящее здоровье пула [0..1] (окно ошибок против бюджета; 0 ≠ cooldown) |
| `llm_cooldown_until_seconds{provider,native_model}` | unix-deadline до возврата провайдера/модели из кулдауна; панель считает остаток `deadline − time()` |
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
см. `modules/observability-prometheus`), `route`, `provider`, `native_model`,
`status`. В `llm-gateway` и `gateway-providers` добавлена переменная `api_key`
(`label_values(llm_requests_total, api_key)`): не-секретный id клиентского
ключа. `llm-gateway` содержит строку «По клиентскому ключу» — RPS и
потребление токенов, сгруппированные `by (api_key)` (в keyless-режиме это
одна серия с пустым `api_key`). `native_model` — реальный id модели у
провайдера; логическая `model`
остаётся в метриках, но дашборды не фильтруются по ней (она одинакова у всех
провайдеров и скрывала, кто ответил). `request_id` — единственная текстовая
переменная, для Loki-запросов только; никогда не label (F12 низкая cardinality).
Loki-поток не имеет `environment` label, поэтому Loki-панели не фильтруются по
среде. Панели используют `$__rate_interval` (Prometheus) и `$__rate_interval`/
`$__range` (Loki). Фильтр `request_id` — точное равенство
(`request_id="${request_id}"`): пустая переменная оставляет панель пустой
вместо вывода всего потока.
