# Lattice LLM Gateway

Небольшой OpenAI-compatible HTTP proxy на Go. Он использует
[Bifrost Core](https://github.com/maximhq/bifrost) через Go API как provider execution layer, но
сам владеет клиентским API, logical models, model discovery и routing policy Lattice.

Пакет не содержит встроенных provider names, logical model IDs или mappings. Они полностью
задаются конфигурацией конкретного deployment. Клиенты видят только объявленные logical models и
не получают native model IDs, provider identities, внутренние URLs или provider credentials.

## API

| Endpoint | Назначение |
| --- | --- |
| `GET /healthz` | Минимальный health check без раскрытия topology |
| `GET /v1/models` | Только logical models, выведенные из скомпилированных routing plans |
| `POST /v1/chat/completions` | OpenAI Chat Completions, streaming и non-streaming |
| `POST /v1/responses` | OpenAI Responses API, streaming и non-streaming |
| `POST /admin/models/refresh` | Немедленное обновление provider-scoped model catalogs |

Если `client_api_key` непустой, все `/v1/*` и `/admin/*` endpoints требуют:

```text
Authorization: Bearer <client_api_key>
```

`/healthz` намеренно возвращает только `{"status":"ok"}`.

## Routing

`routing_rules` — плоская упорядоченная таблица rules. Каждый rule принадлежит именованному
route (поле `route`) и выполняет одно действие с одной ответственностью; физически конфигурация
остаётся плоским массивом, никаких вложенных `routes`/`plans`/`children` нет. Route — просто
строковый scope (`standard`, `standard.retry`, `standard.fallback`); точки в имени не имеют
runtime-семантики. Rules одного route выполняются в порядке появления в глобальном списке.
Retry/fallback/hedge — явные переходы на другой именованный route через `target`; целевой route
сам описывает свои фильтры, providers, mapping и race.

Канонический порядок внутри route:

```text
filter → map → rank → lease → affinity → race → retry/hedge → semaphore → timeout
```

Transition graph (`standard → standard.retry` for retry, `standard → standard.fallback` for
fallback, `standard → standard.hedge` for a latency alternative):

```text
request(model=standard) → standard → timeout? → standard.retry → (attempts) → timeout? → standard.fallback
                                         └──────────────→ standard.hedge (concurrently, after `after`)
```

- `filter` ограничивает применимость route или текущую provider selection; это typed declarative
  primitive без expression language. Одна rule объявляет ровно одну dimension в `where`:
  - `where.model = {"eq": "standard"}` делает route entry route для logical model `standard`
    (request-level applicability и discovery);
  - `where.provider = {"in": [...], "not_in": [...], "unused": true}` строит свежую selection из
    provider universe route: `in` выбирает из universe, `not_in` исключает, `unused` (routing
    policy, не скрытое свойство retry) ограничивает pool providers, ещё не использованными
    текущим request graph. Первый provider filter не удаляет остальных providers навсегда:
    следующий `filter` снова выбирает из universe, поэтому разные provider groups могут получать
    разные native модели через последовательность `filter + map`;
  - `where.error = {"in": ["429", "5xx", "timeout", "connection_error"]}` гейтит переходы в route:
    destination владеет применяемостью (not substring matching; строго typed failure classes);
  - `where.attempt = {"lt": N}` ограничивает вход в route номером текущей попытки.
- `map` привязывает текущую provider selection к одному native model ID и добавляет готовые
  target-пары `(provider ID, native model)` в route pool. Provider selection целиком остаётся в
  предшествующем `filter`; `map` — только transformation. Один provider не может встретиться в
  одном pool более одного раза;
- `rank` сортирует pending pool по `strategy = "priority"`; `race` делает immutable snapshot и
  задаёт размер race batch (0 = весь pool);
- `lease` временно поднимает победителя в начало ranking. Lease scoped по logical model,
  продлевается успешным ответом (`renew_on_success`) и освобождается по настроенным hard
  failures (`release_on`) либо после `release_after_slow_starts` последовательных превышений
  `slow_start`;
- `affinity` закрепляет stateful Responses chain за provider, вернувшим `conversation` или
  `previous_response_id`. Chat Completions никогда не получает affinity; `prompt_cache_key` не
  считается session identifier. Known affinity сужает route до одного provider и при его отказе
  завершает request fail closed (без state replay; cross-provider stateful retry невозможен) —
  весь route graph, включая переходы, подавлен. Unknown identifier игнорируется
  (`on_missing = "ignore"`);
- `race` берёт первые `count` targets из snapshot и запускает их одновременно (first-success
  semantics);
- `retry` — bounded repeated transition в named subroute: только `target`, `attempts` и
  `backoff`. Применимость (какие классы ошибок retryable, какие providers и native) живёт в
  destination route (`standard.retry`), а не в retry. `scope`/`count`/`on` удалены: нужные
  providers явно фильтруются retry route, повтор provider исключается политикой `unused`.
  Следующий entry запускается после backoff и только если terminal failure входит в filter
  destination. Возвращаемый класс выбирается детерминированно;
- `fallback` — one-shot переход в named subroute через `target` (обычно `standard.fallback`);
  это обычная точка в графе, скомпилированная тем же механизмом, что и retry, без отдельного
  «second stage» в compiler state;
- `hedge` — latency-переход: если winner не появился за `after`, target route (`standard.hedge`)
  стартует параллельно, пока ветки ещё выполняются. Hedge не клонирует полный pool, не повторяет
  used providers и не задерживает быстрый terminal failure;
- `semaphore` — request-wide safety action: `max_calls`, `max_in_flight`,
  `max_calls_per_provider` ограничивают суммарные/одновременные/на-провайдера upstream calls на
  весь route graph (share не сбрасывается при входе в subroute);
- `timeout` ограничивает весь route graph, включая retry backoff и fallback. Для streaming он
  ограничивает выбор winner, но не обрывает уже выбранный успешный stream. Абсолютный deadline
  общий для запроса.

Пример (Nix-форма):

```nix
{ route = "standard"; action = "filter"; where = { model = { eq = "standard"; }; }; }
{ route = "standard"; action = "filter"; where = { provider = { "in" = [ "gonka-proxy" "hyperfusion" ]; }; }; }
{ route = "standard"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
{ route = "standard"; action = "race"; count = 2; }
{ route = "standard"; action = "retry"; target = "standard.retry"; attempts = 2; }

{ route = "standard.retry"; action = "filter";
  where = { error = { "in" = [ "429" "5xx" "timeout" "connection_error" ]; }; }; }
{ route = "standard.retry"; action = "filter"; where = { provider = { "in" = allProviders; unused = true; }; }; }
{ route = "standard.retry"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
{ route = "standard.retry"; action = "race"; count = 1; }

{ route = "standard"; action = "fallback"; target = "standard.fallback"; }
{ route = "standard.fallback"; action = "filter";
  where = { error = { "in" = [ "model_not_found" "429" "5xx" "timeout" "connection_error" ]; }; }; }
{ route = "standard.fallback"; action = "filter"; where = { provider = { "in" = [ "hyperfusion" ]; }; }; }
{ route = "standard.fallback"; action = "map"; native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731"; }
{ route = "standard.fallback"; action = "race"; count = 1; }
```

Одна ветка, вернувшая ошибку при живой другой ветке, сама по себе не создаёт новый call: ранний
overlap контролирует только `hedge`. Streaming и non-streaming пути гарантируют одинаковые
invariants: initial race получает только top `count`, каждый route — только свои target-пары,
`unused` не повторяет used providers, и после winner/cancellation/timeout/исчерпания semaphore
budget новые upstream calls не стартуют. Локально отклонённые каталогом target-пары (см.
discovery ниже) никогда не стартуют upstream call и не расходуют `max_calls`/
`max_calls_per_provider`, поэтому провайдер с другим native alias в subroute всё ещё достижим.

Provider с retryable failure получает cooldown, по умолчанию 15 секунд. Охлаждённые providers не
входят в candidate pool; если охлаждаются все, gateway fail-open пробует pool снова. Отменённые
losers не влияют ни на cooldown, ни на lease.

Compilation строго валидирует граф перед запуском: пустой `route`, route без race, `map` без
активной provider selection, duplicate provider в pool, `race.count` сверх pool, missing target,
orphan subroute, duplicate entry model, semaphore/timeout на subroute (эти действия request-wide и
живут на entry route) и routing cycles дают точную configuration error с route, action и индексом
rule.

### Typed routing rules

Внешний `RoutingRule`-union отсутствует: каждый action — отдельный Go type со своими полями,
валидацией и применением к compiler state. JSON decoder читает минимальный envelope `route` +
`action` и декодирует тот же объект в конкретный type с `DisallowUnknownFields` — неизвестный
action, неизвестное поле и поле чужого action отклоняются на decode boundary. `map`/`retry`/
`fallback`/`hedge` больше не несут provider lists, `scope`, `count`, `on` или `fallback_strategy`:
эти поля удалены и при наличии fail fast. Compiled scheduler зависит только от immutable
routing graph (map route name → `compiledRoute`; переходы — скомпилированные ссылки на target
route), не от JSON DTO. Nix `routingRules` — discriminated union: каждый entry валидируется своим
action-подмодулем при evaluation и генерирует только принадлежащие ему поля плюс envelope
`route`/`action`.

Добавление нового action не трогает общий union-struct и центральный compiler switch (его нет):
нужно только зарегистрировать action в `ruleRegistry` (`rule.go`, позиция в pipeline + factory),
написать новый файл `rule_<action>.go` (тип, `apply(*stageContext)`, defaults/validation),
добавить action-подмодуль в Nix `options.nix` и покрыть новый action тестами в
`rule_<action>_test.go` (положительный decode/compile и отрицательные кейсы).

## Model discovery

Discovery provider-scoped: каждый provider владеет своим catalog snapshot и refresh state.
`models_url` может явно направить discovery на независимый совместимый endpoint:

```text
provider-a.inference_url → https://inference-a.example/v1
provider-a.models_url    → https://catalog.example/v1/models
provider-b.inference_url → https://inference-b.example/v1
provider-b.models_url    → отсутствует; выводится как https://inference-b.example/v1/models
```

`inference_url` задаёт **полный** путь до OpenAI-совместимой точки входа и включает версионный
сегмент. Gateway не добавляет `/v1` автоматически: к `inference_url` приклеивается только операция
(`/chat/completions`, `/responses`). Поэтому provider может хостить API под произвольным
маршрутизированным префиксом: `https://…/functions/v1/gonka` даст upstream
`/…/functions/v1/gonka/chat/completions`, а обычный OpenAI-прокси с `inference_url`
`https://api.proxy.gonka.gg/v1` даст `/v1/chat/completions`.

Для provider с `base_provider: openai`, если `models_url` не задан, gateway добавляет к той же
полной базе только `/models`. Неявный каталог использует provider `api_key`; явно заданный
`models_url` использует только отдельный `models_api_key`. Для остальных Bifrost adapters
discovery требует явного `models_url`.

Каждый executable target валидируется по точному совпадению native ID с каталогом конкретного
provider перед dispatch:

- last-known-good snapshot есть и native присутствует — target допустим;
- snapshot есть, native отсутствует — target завершается typed классом `model_not_found` и может
  активировать настроенный fallback;
- inferred catalog недоступен и snapshot ещё не получен — сохраняется optimistic вызов явно
  настроенного native ID;
- explicit `models_url` недоступен без last-known-good — fail closed;
- неудачный refresh не уничтожает last-known-good конкретного provider.

Лексикографический fallback на произвольную модель из каталога удалён полностью: resolver никогда
не выбирает несвязанный native ID. `model_not_found` — явный опт-ин класс в `on` для retry и
fallback; отсутствие native в snapshot классифицируется надёжно, без поиска подстрок в тексте
ошибок.

Catalog обновляется каждые 10 минут или вручную. Неуспешный refresh сохраняет last-known-good
snapshot.

## Пример конфигурации: Gonka

Standalone binary поддерживает literal secrets и ссылки `env.VARIABLE_NAME`. NixOS module вместо
этого собирает приватный `/run/llm-gateway/config.json` через systemd `LoadCredential`. Ни имена
`gonka-*`, ни модели `stupid`/`standard` не являются требованиями package — это только пример
конкретного deployment.

```json
{
  "host": "127.0.0.1",
  "port": 9208,
  "client_api_key": "env.LLM_GATEWAY_CLIENT_KEY",
  "catalog_refresh_interval": "10m",
  "providers": [
    {
      "id": "gonka-proxy",
      "base_provider": "openai",
      "inference_url": "https://proxy.gonka.gg/v1",
      "api_key": "env.PROXY_GONKA_GG_API_KEY",
      "priority": 10,
      "cooldown": "15s",
      "request_timeout": "60s"
    },
    {
      "id": "gonka-openbroker",
      "base_provider": "openai",
      "inference_url": "https://api.openbroker.gonka.gg/v1",
      "models_url": "https://proxy.gonka.gg/v1/models",
      "api_key": "env.OPENBROKER_GONKA_GG_API_KEY",
      "models_api_key": "env.PROXY_GONKA_GG_API_KEY",
      "priority": 10,
      "cooldown": "15s",
      "request_timeout": "60s"
    }
  ],
    "routing_rules": [
    {"route": "stupid", "action": "filter", "where": {"model": {"eq": "stupid"}}},
    {"route": "stupid", "action": "filter", "where": {"provider": {"in": ["gonka-proxy", "gonka-openbroker"]}}},
    {"route": "stupid", "action": "map", "native": "MiniMaxAI/MiniMax-M2.7"},
    {"route": "stupid", "action": "rank", "strategy": "priority"},
    {"route": "stupid", "action": "lease",
      "source": "winner", "duration": "10m", "renew_on_success": true,
      "release_on": ["429", "5xx", "timeout", "connection_error"],
      "release_after_slow_starts": 3, "slow_start": "3s"},
    {"route": "stupid", "action": "affinity",
      "sources": ["responses.conversation", "responses.previous_response_id"],
      "ttl": "24h", "on_missing": "ignore", "on_provider_failure": "fail-closed"},
    {"route": "stupid", "action": "race", "count": 2},
    {"route": "stupid", "action": "retry",
      "target": "stupid.retry", "attempts": 2,
      "backoff": {"type": "exponential", "initial": "200ms", "max": "1s"}},
    {"route": "stupid", "action": "hedge", "after": "3s", "target": "stupid.hedge"},
    {"route": "stupid", "action": "semaphore",
      "max_calls": 4, "max_in_flight": 3, "max_calls_per_provider": 1},
    {"route": "stupid", "action": "timeout", "duration": "60s"},
    {"route": "stupid.retry", "action": "filter",
      "where": {"error": {"in": ["429", "5xx", "timeout", "connection_error", "invalid_response"]}}},
    {"route": "stupid.retry", "action": "filter",
      "where": {"provider": {"in": ["gonka-proxy", "gonka-openbroker"], "unused": true}}},
    {"route": "stupid.retry", "action": "map", "native": "MiniMaxAI/MiniMax-M2.7"},
    {"route": "stupid.retry", "action": "rank", "strategy": "priority"},
    {"route": "stupid.retry", "action": "race", "count": 1},
    {"route": "stupid.hedge", "action": "filter",
      "where": {"provider": {"in": ["gonka-proxy", "gonka-openbroker"], "unused": true}}},
    {"route": "stupid.hedge", "action": "map", "native": "MiniMaxAI/MiniMax-M2.7"},
    {"route": "stupid.hedge", "action": "rank", "strategy": "priority"},
    {"route": "stupid.hedge", "action": "race", "count": 1},

    {"route": "standard", "action": "filter", "where": {"model": {"eq": "standard"}}},
    {"route": "standard", "action": "filter", "where": {"provider": {"in": ["gonka-proxy", "gonka-openbroker"]}}},
    {"route": "standard", "action": "map", "native": "deepseek-ai/DeepSeek-V4-Flash-0731"},
    {"route": "standard", "action": "rank", "strategy": "priority"},
    {"route": "standard", "action": "lease",
      "source": "winner", "duration": "10m", "renew_on_success": true,
      "release_on": ["429", "5xx", "timeout", "connection_error"],
      "release_after_slow_starts": 3, "slow_start": "3s"},
    {"route": "standard", "action": "affinity",
      "sources": ["responses.conversation", "responses.previous_response_id"],
      "ttl": "24h", "on_missing": "ignore", "on_provider_failure": "fail-closed"},
    {"route": "standard", "action": "race", "count": 2},
    {"route": "standard", "action": "retry",
      "target": "standard.retry", "attempts": 2,
      "backoff": {"type": "exponential", "initial": "200ms", "max": "1s"}},
    {"route": "standard", "action": "hedge", "after": "3s", "target": "standard.hedge"},
    {"route": "standard", "action": "semaphore",
      "max_calls": 4, "max_in_flight": 3, "max_calls_per_provider": 1},
    {"route": "standard", "action": "timeout", "duration": "60s"},
    {"route": "standard.retry", "action": "filter",
      "where": {"error": {"in": ["429", "5xx", "timeout", "connection_error", "invalid_response"]}}},
    {"route": "standard.retry", "action": "filter",
      "where": {"provider": {"in": ["gonka-proxy", "gonka-openbroker"], "unused": true}}},
    {"route": "standard.retry", "action": "map", "native": "deepseek-ai/DeepSeek-V4-Flash-0731"},
    {"route": "standard.retry", "action": "rank", "strategy": "priority"},
    {"route": "standard.retry", "action": "race", "count": 1},
    {"route": "standard.hedge", "action": "filter",
      "where": {"provider": {"in": ["gonka-proxy", "gonka-openbroker"], "unused": true}}},
    {"route": "standard.hedge", "action": "map", "native": "deepseek-ai/DeepSeek-V4-Flash-0731"},
    {"route": "standard.hedge", "action": "rank", "strategy": "priority"},
    {"route": "standard.hedge", "action": "race", "count": 1},

    {"route": "standard", "action": "fallback", "target": "standard.fallback"},
    {"route": "standard.fallback", "action": "filter",
      "where": {"error": {"in": ["model_not_found", "429", "5xx", "timeout", "connection_error"]}}},
    {"route": "standard.fallback", "action": "filter",
      "where": {"provider": {"in": ["gonka-openbroker"]}}},
    {"route": "standard.fallback", "action": "map", "native": "gonka/deepseek-ai/DeepSeek-V4-Flash-0731"},
    {"route": "standard.fallback", "action": "rank", "strategy": "priority"},
    {"route": "standard.fallback", "action": "race", "count": 1}
  ]

## Запуск

```sh
go run . --config /path/to/config.json serve
```

или через Nix package:

```sh
nix build .#packages.x86_64-linux.llm-gateway
```

На NixOS runtime настраивается через
[`modules/llm-gateway`](../../modules/llm-gateway/README.md). Homelab mapping находится в
[`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix).

## Проверка

```sh
go test ./...
go test -race ./...
nix flake check --no-build
```

Тесты покрывают:

- реальный Bifrost custom-provider path для Chat и Responses;
- оба streaming API;
- typed `filter`: model eq, provider in/not_in/unused, error in, attempt lt, несколько
  последовательных filters, unknown filter field, unknown operator, invalid value type,
  unknown error class, empty in, missing где-dimension;
- mapping: filter a,b → map X; filter c → map Y; каждый provider получает свой native, duplicate
  provider в одном pool отклоняется;
- entry route: model=standard → standard; model=stupid → stupid; unknown model → явная ошибка без
  dispatch; subroutes никогда не становятся logical models;
- bounded race: ровно top-`count` targets, first-success и игнорирование первой ошибки;
- cancellation проигравшего provider и client disconnect;
- retry subroute: 429 → retry route applicable → retry call; 400 → не applicable → оригинальный
  terminal failure без нового call; attempts и backoff; unused pool exhaustion возвращает
  оригинал;
- fallback subroute: model_not_found/5xx → fallback; 400 → не applicable; общий semaphore budget;
- nested transitions standard → standard.fallback → standard.fallback.retry;
- hedge, стартующий только target route после delay, без клонирования pool и без задержки
  быстрого terminal failure;
- semaphore bounds `max_calls`, `max_in_flight`, `max_calls_per_provider` при race, retry, hedge,
  timeout и client cancellation;
- provider-scoped catalog semantics: exact-match-only, partial refresh независим по provider,
  last-known-good, inferred optimistic и explicit fail-closed;
- `model_not_found` переводит выполнение на явно настроенный fallback subroute и никогда не
  выбирает несвязанную модель; dual-alias provider (один native в entry, другой в fallback) не
  вызывает один endpoint дважды;
- lease acquire/renew/expire/release, slow-start threshold и нейтральность loser cancellations;
- Responses affinity: Chat без affinity, `previous_response_id`, conversation boundary,
  missing/expired mapping, pinned success/failure (fail-closed, весь граф переходов подавлен) и
  отсутствие cross-provider stateful retry;
- persistence opaque affinity mapping через файл mode 0600;
- config validation: поля/порядок, empty pool, duplicate provider, map без selection, race без
  pool, missing race/target, orphan route, duplicate entry model, routing cycles, semaphore/timeout
  on subroute, cascade rejection всех legacy полей (`match`, `map.providers`, `retry.scope/count/on`,
  `fallback.on/fallbackStrategy`);
- logical/native model rewrite и удаление Bifrost routing metadata из client responses;
- отсутствие новых upstream calls после winner/cancellation/timeout/semaphore exhaustion.

## Безопасность

- Provider secrets не входят в публичный Nix config и Git.
- Runtime config имеет mode `0600` и находится в `/run/llm-gateway`.
- Bifrost content logging не включён; executor использует silent logger. Собственные
  структурированные логи gateway не содержат request body, prompt, headers или credentials.
- Ошибки клиенту содержат только безопасный error class, без internal URL, key или native ID.
- Provider-facing raw OpenAI body получает native model только после проверки logical model.
- `/v1/models` публикует только logical IDs, выведенные из скомпилированных plans; native IDs и
  provider metadata не попадают в client responses и безопасные ошибки.

Полный план и незавершённые шаги cutover находятся в
[`f7-07-bifrost-go-proxy.md`](../../docs/roadmap/f7-llm-gateway/f7-07-bifrost-go-proxy.md).
