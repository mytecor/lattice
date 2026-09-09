# Декомпозировать provider routing и ограничить upstream fanout

Фича: [F7 — LLM gateway](./README.md). Follow-up к реализации собственного gateway в
[f7-07](./f7-07-bifrost-go-proxy.md), выявленный во время интерактивной проверки Pi из
[f8-04](../f8-pi-runtime/f8-04-interactive-acceptance.md).

## Контекст

Текущий homelab route для логических моделей `stupid` и `standard` объединяет шесть provider
instances в одну access group `gonka`. Каждый streaming attempt выполняет полный `race` всей
группы, а `hedge` вместе с `retry attempts = 3` держит предыдущие attempts активными и запускает
новые поколения race. Один клиентский запрос поэтому способен породить до 24 upstream calls.

Живая Pi-сессия `01a08399-a696-7417-a65d-84c32695b2c3` дала 12 gateway requests и 270 upstream
calls, то есть в среднем 22,5 upstream calls на один completion. Hyperfusion выиграл все 12
запросов с meaningful TTFT 1,13–2,60 секунды. Остальные providers не выиграли ни разу; Dahl и
Gonkarouter суммарно вернули 32 ответа `429`. После последнего рестарта gateway Hyperfusion также
выиграл первые 16 из 16 запросов.

Проблема состоит из двух независимых множителей:

- initial race одновременно запускает все provider instances;
- hedged retry повторяет весь race, включая уже запущенные providers, и обходит cooldown.

Logical model нельзя делить на provider-specific варианты вроде `standard-fast`: topology и
текущий лидер должны оставаться внутренней политикой gateway. Нельзя также привязывать решение к
Pi: Chat Completions не содержит стандартного session identifier. Affinity разрешена только по
стандартным state identifiers Responses API.

## Решение

Сохранить плоский `routing_rules` pipeline и разделить его на небольшие actions с одной
ответственностью:

```text
pool → rank → lease → affinity → race → retry → hedge → semaphore → timeout
```

### `pool`

Route-creating action формирует candidate pool из access groups. Элементом pool является готовая
target-пара `(provider instance, resolved native model)`, а не только provider или model.

```nix
{
  model = "standard";
  action = "pool";
  accessGroups = [ "gonka" ];
}
```

### `rank`

Сортирует candidate pool. В рамках этой задачи реализуется `strategy = "priority"`; контракт
должен допускать будущий ranking по observed meaningful TTFT без изменения остальных actions.

```nix
{
  model = "standard";
  action = "rank";
  strategy = "priority";
}
```

### `lease`

Временно поднимает победителя в начало ranking. Lease scoped по logical model, продлевается
успешным ответом и освобождается по настроенным hard failures либо после нескольких превышений
meaningful TTFT. Отмена loser не считается failure или slow observation.

```nix
{
  model = "standard";
  action = "lease";
  source = "winner";
  duration = "10m";
  renewOnSuccess = true;
  releaseOn = [ "429" "5xx" "timeout" "connection_error" ];
  releaseAfterSlowStarts = 3;
  slowStart = "3s";
}
```

### `affinity`

Закрепляет stateful Responses chain за тем provider, которому принадлежат `conversation` или
`previous_response_id`. Gateway запоминает соответствие возвращённого `response.id` выбранному
provider и использует его при следующем request. Chat Completions не получает affinity: gateway
не хеширует `messages`, не использует Pi-specific metadata и не трактует `prompt_cache_key` как
session identifier.

Known affinity сужает route до одного provider. Provider-owned state identifier нельзя отправлять
другому upstream; при его отказе gateway завершает request fail closed, пока не реализован
отдельный state replay. Affinity identifiers и request bodies не попадают в logs.

```nix
{
  model = "standard";
  action = "affinity";
  sources = [
    "responses.conversation"
    "responses.previous_response_id"
  ];
  ttl = "24h";
  onMissing = "ignore";
  onProviderFailure = "fail-closed";
}
```

### `race`

Берёт первые `count` unused targets из подготовленного pool и запускает их одновременно. Race
сохраняет first-success semantics: первая branch error не завершает request, streaming winner
выбирается по первому meaningful event, losers отменяются.

```nix
{
  model = "standard";
  action = "race";
  count = 2;
}
```

### `retry`

Остаётся отдельным action. `scope = "same"` повторяет исходную выборку; `scope = "next"` берёт
следующие unused targets из ranked pool. `count` задаёт размер retry batch, `attempts` — число
дополнительных batches.

Следующий batch запускается после retry backoff, только если все terminal failures активной wave
входят в `on`. Non-retryable failure останавливает route, а возвращаемый класс выбирается по
фиксированному приоритету независимо от порядка завершения goroutines. Ошибка одной branch при
другой живой branch сама по себе не создаёт новый call; ранний overlap в этом случае контролируется
только `hedge`. При `scope = "next"` provider не используется повторно.

```nix
{
  model = "standard";
  action = "retry";
  scope = "next";
  count = 1;
  attempts = 2;
  on = [ "429" "5xx" "timeout" "connection_error" "invalid_response" ];
  backoffInitial = "200ms";
  backoffMax = "1s";
}
```

### `hedge`

Разрешает следующему retry batch начаться до завершения текущих branches, если winner не появился
за `after`. Hedge использует next batch из compiled route; он не клонирует полный pool, не
повторяет used providers и не переиспользует retry backoff как hedge delay.

```nix
{
  model = "standard";
  action = "hedge";
  after = "3s";
}
```

### `semaphore`

Request-wide safety action. `maxCalls` ограничивает суммарное число upstream calls initial race,
retry, hedge и fallback, а `maxCallsPerProvider` — calls к одному provider внутри клиентского
request; оба счётчика монотонны в рамках request (бюджет после использования не возвращается). `maxInFlight`
ограничивает одновременно выполняемые calls и реализуется через живой счётчик активных branches:
слот освобождается, когда branch завершается terminal result или фактическим завершением
cancellation, поэтому перекрытая permit-дефицитом ветка может стартовать на следующей hedge-волне.

```nix
{
  model = "standard";
  action = "semaphore";
  maxCalls = 4;
  maxInFlight = 3;
  maxCallsPerProvider = 1;
}
```

### `timeout`

Ограничивает весь скомпилированный route единым абсолютным deadline, включая retry backoff и
fallback, и сохраняет существующую error classification. Для streaming deadline ограничивает
выбор winner, но не обрывает уже выбранный успешный stream.

```nix
{
  model = "standard";
  action = "timeout";
  duration = "60s";
}
```

## Целевая homelab-конфигурация

Одинаковый pipeline применяется к `stupid` и `standard`. Начальный priority provider instances:

| Provider | Priority |
| --- | ---: |
| `hyperfusion` | 100 |
| `gonka-proxy` | 50 |
| `gonka-openbroker` | 40 |
| `gonka-api` | 30 |
| `dahl` | 20 |
| `gonkarouter` | 10 |

Для каждой logical model:

```nix
routingRules = [
  { model = "standard"; action = "pool"; accessGroups = [ "gonka" ]; }
  { model = "standard"; action = "rank"; strategy = "priority"; }
  {
    model = "standard";
    action = "lease";
    source = "winner";
    duration = "10m";
    renewOnSuccess = true;
    releaseOn = [ "429" "5xx" "timeout" "connection_error" ];
    releaseAfterSlowStarts = 3;
    slowStart = "3s";
  }
  {
    model = "standard";
    action = "affinity";
    sources = [ "responses.conversation" "responses.previous_response_id" ];
    ttl = "24h";
    onMissing = "ignore";
    onProviderFailure = "fail-closed";
  }
  { model = "standard"; action = "race"; count = 2; }
  {
    model = "standard";
    action = "retry";
    scope = "next";
    count = 1;
    attempts = 2;
    on = [ "429" "5xx" "timeout" "connection_error" "invalid_response" ];
    backoffInitial = "200ms";
    backoffMax = "1s";
  }
  { model = "standard"; action = "hedge"; after = "3s"; }
  {
    model = "standard";
    action = "semaphore";
    maxCalls = 4;
    maxInFlight = 3;
    maxCallsPerProvider = 1;
  }
  { model = "standard"; action = "timeout"; duration = "60s"; }
];
```

Expected schedule при отсутствии affinity:

```text
t=0s       top-2 targets стартуют как race
t=3s       при отсутствии winner стартует один следующий unused target, если есть permit
t=6s       может стартовать ещё один unused target, если есть permit и общий budget
winner     все losers отменяются, winner получает или продлевает lease
```

При `race count = 2`, двух next retries и целевом semaphore request никогда не создаёт больше
четырёх upstream calls, больше трёх одновременно выполняемых calls или повторный call к одному
provider.

## Обратная совместимость

- Старое `{ action = "race"; accessGroups = [ ... ]; }` нормализуется в implicit
  `pool(accessGroups) → rank(priority) → race(count=all)`.
- Старый `retry` без `scope` получает `scope = "same"` и сохраняет прежнее повторение stage.
- Новый `retry scope = "next"` никогда не повторяет used provider.
- Старый parameterless `hedge` либо сохраняется только для legacy-normalized route, либо
  отклоняется с точной migration error; выбранный вариант закрепляется config tests и
  документацией.
- Целевой homelab config полностью мигрируется на явные actions и больше не использует legacy
  shorthand.

## Что сделать

### 1. Typed contract и compilation

- [x] Расширить typed Go config в [config.go](./../../../packages/llm-gateway/config.go) и Nix
  options в [options.nix](./../../../modules/llm-gateway/options.nix) actions `pool`, `rank`,
  `lease`, `affinity`, `race`, `retry`, `hedge`, `semaphore`, `timeout` и их поля.
- [x] Добавить строгую validation допустимых полей и порядка actions; ошибка содержит model,
  индекс rule и конкретную причину.
- [x] Отделить внешние rules от compiled candidate pool, ranking, dispatch batches, retry/hedge
  schedule, semaphore limits, lease и affinity state.
- [x] Реализовать legacy normalization для `race accessGroups` и `retry` без `scope`.
- [x] Не переносить Nix-представление напрямую в runtime scheduler.

### 2. Единый bounded scheduler

- [x] Переработать [router.go](./../../../packages/llm-gateway/router.go) и
  [router_stream.go](./../../../packages/llm-gateway/router_stream.go): initial race получает
  только top `count`, retry next получает unused targets, hedge запускает следующий batch.
- [x] Удалить текущую механику hedged full-race generations и обход cooldown для каждого нового
  streaming attempt.
- [x] Обеспечить одинаковые routing invariants для streaming и non-streaming paths.
- [x] Выбирать streaming winner только по meaningful content, reasoning или tool-call event;
  cancellations losers не влияют на health или lease.
- [x] Не запускать новые goroutines и upstream calls после winner, client cancellation, timeout
  или исчерпания semaphore budget.
- [x] Не зависать при pool exhaustion, частично доступном semaphore permit или медленном
  завершении cancelled executor call.

### 3. Lease и affinity state

- [x] Реализовать потокобезопасный lease store с injectable clock: acquire, renew, expiry,
  hard-failure release и consecutive slow-start release.
- [x] Измерять slow start до первого meaningful event; для non-streaming использовать время до
  валидного успешного response.
- [x] Расширить request parsing в [server.go](./../../../packages/llm-gateway/server.go) только
  полями Responses `conversation` и `previous_response_id`; Chat body не используется как
  affinity key.
- [x] Извлекать `response.id` из обычного Responses JSON и streaming SSE, связывать его с winner
  provider и продлевать mapping TTL без сохранения prompt или content.
- [x] Определить и реализовать безопасное хранение opaque affinity mapping через service restart;
  файл или state database доступен только пользователю gateway и не содержит prompts, keys или
  provider URLs.
- [x] Для known affinity ограничивать route pinned provider и fail closed при его отказе.
- [x] Зафиксировать границу поддержки `conversation`: либо добавить необходимый proxy endpoint,
  либо принимать только уже известные gateway mapping и документированно обрабатывать unknown ID.

### 4. Nix и homelab migration

- [x] Обновить генерацию runtime JSON в [module config](./../../../modules/llm-gateway/config.nix)
  и assertions для всех новых actions.
- [x] Мигрировать [homelab config](./../../../nodes/mytecor-homelab/config.nix) для `stupid` и
  `standard` на целевой pipeline и новые priorities.
- [x] Обновить evaluation assertions в [tests/default.nix](./../../../tests/default.nix),
  [Bifrost gateway test](./../../../tests/llm-gateway-bifrost.nix) и
  [service test](./../../../tests/llm-gateway-service.nix).
- [x] Убедиться, что generated public config не содержит credentials, affinity identifiers,
  prompts или runtime state.

### 5. Поведенческие тесты

- [x] Покрыть config validation, legacy normalization, pool creation и priority ranking в
  [config tests](./../../../packages/llm-gateway/config_test.go) и
  [router tests](./../../../packages/llm-gateway/router_test.go).
- [x] Доказать, что `race count = N` запускает ровно top-N доступных targets и сохраняет
  first-success/cancellation semantics.
- [x] Доказать `retry scope = same`, `retry scope = next`, отсутствие повторов, корректное
  исчерпание pool и backoff.
- [x] Доказать, что hedge запускает только следующий retry batch после `after`, а не полный pool;
  retryable failure всего active batch может продолжить route раньше hedge timer.
- [x] Доказать semaphore bounds `maxCalls`, `maxInFlight`, `maxCallsPerProvider` при race, retry,
  hedge, timeout и client cancellation.
- [x] Покрыть lease acquire/renew/expire/release, slow threshold и нейтральность loser
  cancellations.
- [x] Покрыть Chat без affinity, Responses `previous_response_id`, conversation boundary,
  missing/expired mapping, pinned success/failure и отсутствие cross-provider stateful retry.
- [x] Покрыть Responses affinity для streaming и non-streaming response IDs в
  [server tests](./../../../packages/llm-gateway/server_test.go).
- [x] Сохранить существующую routing/error/cooldown/discovery/security matrix.

### 6. Документация и проверка

- [x] Обновить фактический routing contract в
  [package README](./../../../packages/llm-gateway/README.md),
  [module README](./../../../modules/llm-gateway/README.md) и
  [ARCHITECTURE.md](./../../../ARCHITECTURE.md).
- [x] Запустить `gofmt` и `go test ./...` в package gateway.
- [x] Запустить `go test -race ./...` для конкурентного scheduler/state.
- [x] Запустить `nix flake check --all-systems --no-build` и доступные gateway/NixOS builds из
  [CONTRIBUTING.md](./../../../CONTRIBUTING.md).
- [x] После изменений обновить code index через `ccc index`, проверить diff и отсутствие
  secrets, prompt values, временных файлов и несвязанных изменений.
- [x] Перед production activation сравнить observed `calls/request`, `max_in_flight`, winner и
  meaningful TTFT с baseline этой задачи; deploy и push выполняются отдельно по запросу.

## Критерий готовности (Definition of Done)

- [x] Целевой homelab config успешно проходит Nix evaluation для обеих logical models.
- [x] Один request при целевом config никогда не создаёт больше четырёх upstream calls, больше
  трёх concurrent calls или повторный call к одному provider.
- [x] Hedge не создаёт полные race generations; retry next использует только unused targets.
- [x] Race и retry остаются явными actions, а pool/rank/lease/affinity/semaphore имеют по одной
  ответственности и независимо тестируются.
- [x] Provider выбирается через общий logical-model route без `standard-fast` и без Pi-specific
  session logic.
- [x] Responses affinity использует только protocol identifiers; Chat работает без affinity;
  известный stateful route не переключается на другой provider без replay.
- [x] Streaming/non-streaming, legacy config, cancellation, cooldown, timeout и semaphore
  invariants подтверждены tests и согласованы с документацией.
- [ ] На live smoke средний upstream fanout снижен с baseline 22,5 до не более 4 calls/request,
  при этом p95 meaningful TTFT не ухудшен более чем на согласованный перед deploy порог.
  (Отдельный шаг перед/после активации; deploy и push по отдельному запросу.)

## Затрагиваемые файлы / слои

- [Gateway package](./../../../packages/llm-gateway/README.md)
- [LLM gateway module](./../../../modules/llm-gateway/README.md)
- [Homelab node config](./../../../nodes/mytecor-homelab/config.nix)
- [Nix evaluation и service tests](./../../../tests/default.nix)
- [Архитектура](./../../../ARCHITECTURE.md)

## Открытые вопросы (решённые при реализации)

- **Conversation boundary.** Первая версия не добавляет `/v1/conversations` proxy: affinity
  принимает только идентификаторы, уже записанные gateway из собственных ответов. Незнакомый
  `conversation_id` трактуется как missing/unknown и обрабатывается по `on_missing = "ignore"`.
  Зафиксировано в `packages/llm-gateway/README.md` и поведенческих тестах.
- **Persistent affinity формат.** Opaque mapping хранится как JSON-файл id → {provider,
  expires_at} с mode `0600`, владелец — пользователь gateway, путь по умолчанию
  `/run/llm-gateway/affinity.json` (`affinity_file` в модуле). Файл переживает service restart
  (RuntimeDirectory сохраняется при restart), очищается при полной остановке/перезагрузке;
  не содержит prompts, keys или provider URLs. Запись атомарная (tmp + rename).
- **Parameterless hedge.** Выбран вариант «отклонять с точной migration error»: `hedge` без
  `after` не принимается, ошибка прямо ссылается на миграцию f7-09. Закреплено config тестом.
- **Допустимый порог ухудшения p95 meaningful TTFT** фиксируется перед deploy после
  воспроизводимого baseline smoke (отдельный шаг).
