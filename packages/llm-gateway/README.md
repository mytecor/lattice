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
| `GET /v1/models` | Только logical models, настроенные в `models` |
| `POST /v1/chat/completions` | OpenAI Chat Completions, streaming и non-streaming |
| `POST /v1/responses` | OpenAI Responses API, streaming и non-streaming |
| `POST /admin/models/refresh` | Немедленное обновление внутренних model catalogs |

Если `client_api_key` непустой, все `/v1/*` и `/admin/*` endpoints требуют:

```text
Authorization: Bearer <client_api_key>
```

`/healthz` намеренно возвращает только `{"status":"ok"}`.

## Routing

`routing_rules` — плоский упорядоченный pipeline. Каждый rule выполняет одно действие с одной
ответственностью и преобразует compiled route. Канонический порядок:

```text
pool → rank → lease → affinity → race → retry → hedge → semaphore → timeout
```

- `pool` формирует candidate pool из перечисленных `access_groups`; элементом pool является
  готовая target-пара (provider instance, resolved native model);
- `rank` сортирует pool по `strategy = "priority"` (порядок гарантирует контракт для будущих
  ранжирований по наблюдаемому meaningful TTFT);
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
- `race` берёт первые `count` unused targets из подготовленного pool и запускает их одновременно
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
  включая legacy fallback;
- `timeout` ограничивает весь скомпилированный route, включая retry backoff и fallback. Для
  streaming он ограничивает выбор winner, но не обрывает уже выбранный успешный stream.

Одна ветка, вернувшая ошибку при живой другой ветке, сама по себе не создаёт новый call: ранний
overlap контролирует только `hedge`. Streaming и non-streaming пути гарантируют одинаковые
invariants: initial race получает только top `count`, retry next — только unused targets, hedge —
следующий batch, и после winner/cancellation/timeout/исчерпания semaphore budget новые upstream
calls не стартуют.

Legacy-нормализация:

- старый `{ action = "race"; access_groups = [...]; }` компилируется в implicit
  `pool(access_groups) → rank(priority) → race(count=all)`;
- старый `retry` без `scope` получает `scope = "same"` и повторяет исходную выборку;
- `{ action = "hedge"; }` без `after` отклоняется с точной migration error — целевой конфиг
  переведён на явные actions.

Provider с retryable failure получает cooldown, по умолчанию 15 секунд. Охлаждённые providers не
попадают в candidate pool; если охлаждаются все, gateway fail-open пробует группу снова.
Отменённые losers не влияют ни на cooldown, ни на lease.

## Model discovery

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
маршрутизированным префиксом, например супрабазовская Edge Function
`https://…/functions/v1/gonka` даст upstream `/…/functions/v1/gonka/chat/completions`, а обычный
OpenAI-прокси с `inference_url` `https://api.proxy.gonka.gg/v1` даст `/v1/chat/completions`.

Для provider с `base_provider: openai`, если `models_url` не задан, gateway добавляет к той же
полной базе только `/models`. Поэтому
`https://api.example/v1` даёт каталог `https://api.example/v1/models`, а
`https://…/functions/v1/gonka` — `https://…/functions/v1/gonka/models`, без повторного `/v1`.
Неявный каталог использует provider `api_key`; явно заданный `models_url` использует только
отдельный `models_api_key`. Успешные каталоги нескольких providers одной access group объединяются.
Если все неявные endpoints недоступны, configured primary model остаётся рабочим; явно заданный
catalog сохраняет fail-closed семантику до появления last-known-good snapshot.
Для остальных Bifrost adapters discovery требует явного `models_url`, поскольку их catalog paths
не следуют единому OpenAI-контракту.

`models_api_key` задаётся отдельно от inference credential. Gateway не переиспользует
`api_key` для явно указанного другого host неявно.

Catalog обновляется каждые 10 минут или вручную. Неуспешный refresh сохраняет last-known-good
snapshot. Если primary model исчезла, gateway детерминированно выбирает первую доступную модель из
той же access group после нормализации, дедупликации и сортировки. Пустая группа работает
fail-closed.

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
      "name": "gonka",
      "base_provider": "openai",
      "inference_url": "https://proxy.gonka.gg/v1",
      "api_key": "env.PROXY_GONKA_GG_API_KEY",
      "priority": 10,
      "cooldown": "15s",
      "request_timeout": "60s"
    },
    {
      "id": "gonka-openbroker",
      "name": "gonka",
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
  "models": [
    {
      "match": {"provider": "gonka", "id": "MiniMaxAI/MiniMax-M2.7"},
      "override": {"id": "stupid"}
    },
    {
      "match": {"provider": "gonka", "id": "deepseek-ai/DeepSeek-V4-Flash-0731"},
      "override": {"id": "standard"}
    }
  ],
  "routing_rules": [
    {
      "match": {"model": "stupid"},
      "action": "pool",
      "access_groups": ["gonka"]
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
      "action": "pool",
      "access_groups": ["gonka"]
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
- bounded race: ровно top-`count` targets, first-success и игнорирование первой ошибки;
- cancellation проигравшего provider и client disconnect;
- retry `scope = "same"` / `scope = "next"`, отсутствие повторного использования provider,
  исчерпание pool и backoff;
- hedge, стартующий только следующий retry batch, а не полный pool;
- semaphore bounds `max_calls`, `max_in_flight`, `max_calls_per_provider` при race, retry, hedge,
  timeout и client cancellation;
- lease acquire/renew/expire/release, slow-start threshold и нейтральность loser cancellations;
- Responses affinity: Chat без affinity, `previous_response_id`, conversation boundary,
  missing/expired mapping, pinned success/failure (fail-closed) и отсутствие cross-provider
  stateful retry;
- persistence opaque affinity mapping через файл mode 0600;
- config validation (поля/порядок), legacy normalization `race accessGroups` и `retry` без `scope`;
- independent discovery URL, manual refresh, last-known-good и fail-closed;
- logical/native model rewrite и удаление Bifrost routing metadata из client responses;
- отсутствие новых upstream calls после winner/cancellation/timeout/semaphore exhaustion.

## Безопасность

- Provider secrets не входят в публичный Nix config и Git.
- Runtime config имеет mode `0600` и находится в `/run/llm-gateway`.
- Bifrost content logging не включён; executor использует silent logger. Собственные
  структурированные логи gateway не содержат request body, prompt, headers или credentials.
- Ошибки клиенту содержат только безопасный error class, без internal URL, key или native ID.
- Provider-facing raw OpenAI body получает native model только после проверки logical model.

Полный план и незавершённые шаги cutover находятся в
[`f7-07-bifrost-go-proxy.md`](../../docs/roadmap/f7-llm-gateway/f7-07-bifrost-go-proxy.md).
