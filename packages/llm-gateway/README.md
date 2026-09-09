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

`routing_rules` — плоский упорядоченный pipeline. Каждый rule выполняет одно действие с одной
ответственностью и преобразует compiled route. Канонический порядок:

```text
map → rank → lease → affinity → race → retry → hedge → semaphore → timeout
```

Опциональный fallback — отдельный второй stage:

```text
map → [rank] → fallback
```

- `map` связывает один native model ID с явным набором provider IDs и добавляет готовые
  target-пары `(provider ID, native model)` в pending candidate pool. Один или несколько
  последовательных `map` формируют pool; один provider не может встретиться в одном pool более
  одного раза даже с разными native IDs. Разные providers одного logical model могут получать
  разные native IDs через последовательные `map`;
- `rank` сортирует pending pool по `strategy = "priority"` (порядок гарантирует контракт для
  будущих ранжирований по наблюдаемому meaningful TTFT). Route-creating action (`race`, или
  `fallback` для второго stage) сохраняет immutable snapshot pool;
- `lease` временно поднимает победителя в начало ranking. Lease scoped по logical model,
  продлевается успешным ответом (`renew_on_success`) и освобождается по настроенным hard
  failures (`release_on`) либо после `release_after_slow_starts` последовательных превышений
  `slow_start` (время до первого meaningful события; для non-streaming — до валидного успешного
  ответа);
- `affinity` закрепляет stateful Responses chain за provider, вернувшим `conversation` или
  `previous_response_id`. Chat Completions никогда не получает affinity; `prompt_cache_key` не
  считается session identifier. Known affinity сужает route до одного provider и при его отказе
  завершает request fail closed (без state replay; cross-provider stateful retry невозможен).
  Unknown identifier игнорируется (`on_missing = "ignore"`). Поддержка `conversation` ограничена
  идентификаторами, которые gateway уже записал из своих ответов: `/v1/conversations` proxy не
  реализован, поэтому незнакомый conversation_id трактуется как unknown и не закрепляет route;
- `race` берёт первые `count` unused targets из snapshot и запускает их одновременно
  (first-success semantics);
- `retry` остаётся отдельным действием: `scope = "same"` повторяет исходную выборку,
  `scope = "next"` берёт следующие unused targets из ranked pool (provider никогда не
  повторяется). `count` задаёт размер retry batch, `attempts` — число дополнительных batches.
  Следующий batch запускается после backoff, только если все terminal failures активной wave
  входят в `on`; смешанная wave с non-retryable ошибкой останавливает route. Возвращаемый класс
  выбирается детерминированно, независимо от порядка завершения goroutines;
- `hedge` разрешает следующему retry batch начаться до завершения текущих branches, если winner
  не появился за `after`. Hedge использует только next batch из compiled route — он не клонирует
  полный pool и не повторяет used providers;
- `semaphore` — request-wide safety action: `max_calls`, `max_in_flight`,
  `max_calls_per_provider` ограничивают суммарные/одновременные/на-провайдера upstream calls,
  включая fallback;
- `timeout` ограничивает весь скомпилированный route, включая retry backoff и fallback. Для
  streaming он ограничивает выбор winner, но не обрывает уже выбранный успешный stream.

Второй stage (fallback) начинается с нового набора `map` после завершения описания предыдущего
stage. Новое mapping не меняет уже скомпилированный primary snapshot. `fallback` использует
snapshot нового pending pool и одновременно задаёт классы ошибок, переводящие с предыдущего
stage:

```nix
{ model = "standard"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731";
  providers = [ "gonka-proxy" "hyperfusion" ]; }
{ model = "standard"; action = "race"; count = 2; }
{ model = "standard"; action = "retry"; scope = "next"; count = 1; attempts = 2;
  on = [ "429" "5xx" "timeout" "connection_error" ]; }

{ model = "standard"; action = "map"; native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731";
  providers = [ "hyperfusion" ]; }
{ model = "standard"; action = "fallback"; fallbackStrategy = "race";
  on = [ "model_not_found" "429" "5xx" "timeout" "connection_error" ]; }
```

Одна ветка, вернувшая ошибку при живой другой ветке, сама по себе не создаёт новый call: ранний
overlap контролирует только `hedge`. Streaming и non-streaming пути гарантируют одинаковые
invariants: initial race получает только top `count`, retry next — только unused targets, hedge —
следующий batch, и после winner/cancellation/timeout/исчерпания semaphore budget новые upstream
calls не стартуют. Локально отклонённые каталогом target-пары (см. discovery ниже) никогда не
стартуют upstream call и не расходуют `max_calls`/`max_calls_per_provider`, поэтому провайдер с
другим native alias в fallback stage всё ещё достижим.

Provider с retryable failure получает cooldown, по умолчанию 15 секунд. Охлаждённые providers не
входят в candidate pool; если охлаждаются все, gateway fail-open пробует pool снова. Отменённые
losers не влияют ни на cooldown, ни на lease.

Compilation строго валидирует порядок `map`, `rank`, `race`, `fallback` и модификаторов: пустой
pool, duplicate provider, dangling `map`, mapping после stage и отсутствие предыдущего stage дают
точную configuration error с logical model и индексом rule.

### Typed routing rules

Внешний `RoutingRule`-union отсутствует: каждый action — отдельный Go type со своими полями,
валидацией и применением к compiler state. JSON decoder читает минимальный envelope с `action` и
декодирует тот же объект в конкретный type с `DisallowUnknownFields` — неизвестный action,
неизвестное поле и поле чужого action отклоняются на decode boundary. Compiled scheduler
зависит только от immutable `Plan`, не от JSON DTO. Nix `routingRules` — discriminated union: каждый entry валидируется своим action-подмодулем при
evaluation и генерирует только принадлежащие ему поля.

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
    {
      "match": {"model": "stupid"},
      "action": "map",
      "native": "MiniMaxAI/MiniMax-M2.7",
      "providers": ["gonka-proxy", "gonka-openbroker"]
    },
    {
      "match": {"model": "stupid"},
      "action": "rank",
      "strategy": "priority"
    },
    {
      "match": {"model": "stupid"},
      "action": "lease",
      "source": "winner",
      "duration": "10m",
      "renew_on_success": true,
      "release_on": ["429", "5xx", "timeout", "connection_error"],
      "release_after_slow_starts": 3,
      "slow_start": "3s"
    },
    {
      "match": {"model": "stupid"},
      "action": "affinity",
      "sources": ["responses.conversation", "responses.previous_response_id"],
      "ttl": "24h",
      "on_missing": "ignore",
      "on_provider_failure": "fail-closed"
    },
    {
      "match": {"model": "stupid"},
      "action": "race",
      "count": 2
    },
    {
      "match": {"model": "stupid"},
      "action": "retry",
      "scope": "next",
      "count": 1,
      "attempts": 2,
      "on": ["429", "5xx", "timeout", "connection_error", "invalid_response"],
      "backoff": {"type": "exponential", "initial": "200ms", "max": "1s"}
    },
    {
      "match": {"model": "stupid"},
      "action": "hedge",
      "after": "3s"
    },
    {
      "match": {"model": "stupid"},
      "action": "semaphore",
      "max_calls": 4,
      "max_in_flight": 3,
      "max_calls_per_provider": 1
    },
    {
      "match": {"model": "stupid"},
      "action": "timeout",
      "duration": "60s"
    },
    {
      "match": {"model": "standard"},
      "action": "map",
      "native": "deepseek-ai/DeepSeek-V4-Flash-0731",
      "providers": ["gonka-proxy", "gonka-openbroker"]
    },
    {
      "match": {"model": "standard"},
      "action": "rank",
      "strategy": "priority"
    },
    {
      "match": {"model": "standard"},
      "action": "lease",
      "source": "winner",
      "duration": "10m",
      "renew_on_success": true,
      "release_on": ["429", "5xx", "timeout", "connection_error"],
      "release_after_slow_starts": 3,
      "slow_start": "3s"
    },
    {
      "match": {"model": "standard"},
      "action": "affinity",
      "sources": ["responses.conversation", "responses.previous_response_id"],
      "ttl": "24h",
      "on_missing": "ignore",
      "on_provider_failure": "fail-closed"
    },
    {
      "match": {"model": "standard"},
      "action": "race",
      "count": 2
    },
    {
      "match": {"model": "standard"},
      "action": "retry",
      "scope": "next",
      "count": 1,
      "attempts": 2,
      "on": ["429", "5xx", "timeout", "connection_error", "invalid_response"],
      "backoff": {"type": "exponential", "initial": "200ms", "max": "1s"}
    },
    {
      "match": {"model": "standard"},
      "action": "hedge",
      "after": "3s"
    },
    {
      "match": {"model": "standard"},
      "action": "semaphore",
      "max_calls": 4,
      "max_in_flight": 3,
      "max_calls_per_provider": 1
    },
    {
      "match": {"model": "standard"},
      "action": "timeout",
      "duration": "60s"
    },
    {
      "match": {"model": "standard"},
      "action": "map",
      "native": "gonka/deepseek-ai/DeepSeek-V4-Flash-0731",
      "providers": ["gonka-openbroker"]
    },
    {
      "match": {"model": "standard"},
      "action": "fallback",
      "fallback_strategy": "race",
      "on": ["model_not_found", "429", "5xx", "timeout", "connection_error"]
    }
  ]
}
```

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
- `map` binding: разные providers одного logical model получают разные natives, последовательные
  `map` аккумулируют pool, duplicate provider в одном pool отклоняется;
- bounded race: ровно top-`count` targets, first-success и игнорирование первой ошибки;
- cancellation проигравшего provider и client disconnect;
- retry `scope = "same"` / `scope = "next"`, отсутствие повторного использования provider,
  исчерпание pool и backoff;
- hedge, стартующий только следующий retry batch, а не полный pool;
- semaphore bounds `max_calls`, `max_in_flight`, `max_calls_per_provider` при race, retry, hedge,
  timeout и client cancellation;
- provider-scoped catalog semantics: exact-match-only, partial refresh независим по provider,
  last-known-good, inferred optimistic и explicit fail-closed;
- `model_not_found` переводит выполнение на явно настроенный fallback и никогда не выбирает
  несвязанную модель; dual-alias provider (один native в primary, другой в fallback) не вызывает
  один endpoint дважды;
- lease acquire/renew/expire/release, slow-start threshold и нейтральность loser cancellations;
- Responses affinity: Chat без affinity, `previous_response_id`, conversation boundary,
  missing/expired mapping, pinned success/failure (fail-closed) и отсутствие cross-provider
  stateful retry;
- persistence opaque affinity mapping через файл mode 0600;
- config validation (поля/порядок, empty pool, duplicate provider, dangling map, mapping после
  stage, отсутствие предыдущего stage);
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
