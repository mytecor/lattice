# Перенести native model mapping и выбор providers в routing pipeline

Фича: [F7 — LLM gateway](./README.md). Продолжает bounded scheduler из
[f7-09](./f7-09-bounded-provider-routing.md) и заменяет отдельный model registry единым
route-centric контрактом.

## Контекст

После [f7-09](./f7-09-bounded-provider-routing.md) gateway компилирует плоский pipeline
`pool → rank → lease → affinity → race → retry → hedge → semaphore → timeout`, однако выбор
native model остаётся вынесен из pipeline:

- Nix-опция `models` задаёт единственный native ID для пары `(logical model, access group)`;
- `pool` и legacy `fallback` выбирают access groups, которые затем раскрываются в provider IDs;
- runtime хранит `mappings[logical][accessGroup] = native`;
- каталоги всех providers одной access group объединяются;
- если primary отсутствует, resolver выбирает первую модель из лексикографически отсортированного
  объединённого каталога.

Эта модель не выражает provider-specific native IDs. Например, один endpoint может принимать
`deepseek-ai/DeepSeek-V4-Flash-0731`, другой — только
`gonka/deepseek-ai/DeepSeek-V4-Flash-0731`, а Hyperfusion принимает оба варианта. Добавление двух
элементов `models` для одной пары `(standard, gonka)` сейчас запрещено Nix assertion и runtime
validation. При этом объединённый catalog не доказывает, что найденный native ID поддерживается
конкретным provider, которому отправляется запрос.

Provider registry и routing policy выполняют разные задачи. Registry должен по-прежнему один раз
описывать transport и shared runtime identity: inference/catalog URL, Bifrost adapter, credentials,
headers, priority, timeout и cooldown. Выбор provider и native model является routing policy и
должен находиться в rules.

## Принятое решение

### `map` как отдельный action

Удалить конфигурационный список `models`. Добавить отдельный action `map`, который связывает один
native ID с явным набором provider IDs:

```nix
{
  model = "standard";
  action = "map";
  native = "deepseek-ai/DeepSeek-V4-Flash-0731";
  providers = [
    "gonka-proxy"
    "gonka-openbroker"
    "hyperfusion"
  ];
}
{
  model = "standard";
  action = "map";
  native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731";
  providers = [ "provider-requiring-gonka-prefix" ];
}
{ model = "standard"; action = "rank"; strategy = "priority"; }
{ model = "standard"; action = "race"; count = 2; }
```

Один или несколько последовательных `map` формируют pending candidate pool из готовых target-пар
`(provider ID, native model)`. `rank` преобразует этот pool, а route-creating action сохраняет его
immutable snapshot. `race` отвечает только за dispatch и не содержит model mapping или provider
selector.

Один provider не может встречаться в одном pending pool больше одного раза даже с разными native
IDs: это предотвращает два одновременных запроса к одному endpoint под разными alias. Если provider
поддерживает несколько native aliases, rule обязан выбрать один из них для конкретного stage.

Для отдельного fallback stage допускается новый набор `map` после завершения описания предыдущего
stage. Новый mapping не меняет уже скомпилированный stage. `fallback` использует snapshot нового
pending pool и одновременно задаёт классы ошибок, переводящие с предыдущего stage:

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

Компилятор должен явно определить границу stage и сохранить bounded invariants из
[f7-09](./f7-09-bounded-provider-routing.md): retry/hedge не клонируют полный pool, semaphore
ограничивает общий fanout, provider cooldown/lease/affinity остаются scoped по стабильному
provider ID. Если существующая реализация `fallback` мешает этой модели, её нужно перевести на
тот же compiled target pool, а не сохранять отдельный legacy путь через access groups.

### Providers остаются registry

Полные provider definitions не переносятся внутрь rules. В rules используются только стабильные
provider IDs. Это сохраняет единую Bifrost-конфигурацию, credential materialization, catalog
refresh, cooldown, lease и health state для одного endpoint во всех logical models.

После миграции access group больше не является routing identity. Удалить `accessGroup`/runtime
provider `name`, group mappings и group assertions, если они не имеют независимого назначения.
Не сохранять скрытое включение нового endpoint в production route только из-за совпавшей группы.

### Provider-scoped model discovery

Catalog snapshots и refresh state индексируются по provider ID. Каждый provider использует свой
явный `modelsUrl` или существующее правило вывода `<inferenceUrl>/models`; credentials policy для
explicit и inferred catalog сохраняется.

Resolver принимает только точное совпадение native ID target с каталогом конкретного provider.
Выбор произвольного первого ID из каталога удалить полностью.

- last-known-good snapshot есть, native присутствует — target допустим;
- snapshot есть, native отсутствует — target завершается `model_not_found` и может активировать
  fallback;
- inferred catalog недоступен и snapshot ещё не получен — сохранить optimistic вызов явно
  настроенного native ID;
- explicit `modelsUrl` недоступен без last-known-good — fail closed;
- неудачный refresh не уничтожает last-known-good конкретного provider.

Добавить typed error class `model_not_found` в runtime и Nix `on`. Надёжно классифицировать
локальное отсутствие native в snapshot. Upstream `400`/`404` переводить в этот класс только при
однозначном структурированном сигнале unsupported/unknown model; не использовать хрупкий поиск
произвольной подстроки в тексте ошибки.

### Logical model contract

Logical IDs выводятся из успешно скомпилированных executable plans. `/v1/models` продолжает
возвращать только unique sorted logical IDs. Неизвестный logical ID отклоняется до upstream call,
а response/SSE sanitization возвращает клиенту logical model и не раскрывает native ID или
provider metadata.

## Что сделать

- [x] Заменить `models` и access-group mapping на action `map` в Nix и runtime JSON contract.
- [x] Перевести compiled pool/stages с provider IDs на immutable target-пары `(provider, native)`.
- [x] Реализовать validation порядка `map`, `rank`, `race`, `fallback` и modifiers: пустой pool,
  duplicate provider, dangling `map`, mapping после stage и отсутствие предыдущего stage должны
  давать точную configuration error с logical model и индексом rule.
- [x] Перевести primary и fallback execution на единый target-pool path без legacy group resolver.
- [x] Удалить access-group routing и мигрировать все repository configurations на явные provider
  IDs; provider transport/credentials оставить в registry.
- [x] Сделать discovery, last-known-good и exact native validation provider-scoped.
- [x] Удалить лексикографический fallback на произвольную модель.
- [x] Добавить `model_not_found` и подключить его к fallback/error policy.
- [x] Сохранить bounded scheduler, priority, lease, affinity, cooldown, semaphore, timeout,
  streaming winner и cancellation semantics из [f7-09](./f7-09-bounded-provider-routing.md).
- [x] Вывести public logical model registry из compiled plans и сохранить sanitization boundary.
- [x] Мигрировать homelab mappings `stupid` и `standard`, включая оба DeepSeek native alias только
  для тех providers, которым они действительно назначены.
- [x] Обновить module/package документацию и migration examples.

## Критерий готовности (Definition of Done)

- [x] В конфигурации отсутствуют отдельные `models` и routing `accessGroups`; каждый executable
  target получен из явного `map(native, providers)`.
- [x] Один stage не вызывает один provider дважды под разными aliases; разные providers одного
  logical model могут получать разные native IDs.
- [x] Hyperfusion с двумя catalog IDs однозначно получает native, выбранный его `map`, без
  лексикографической подстановки и без дублирующего race call.
- [x] Provider-scoped catalog tests доказывают exact-match-only, partial refresh,
  last-known-good, inferred optimistic и explicit fail-closed semantics.
- [x] `model_not_found` переводит выполнение на явно настроенный fallback и никогда не выбирает
  несвязанную модель.
- [x] `/v1/models`, Chat Completions и Responses продолжают публиковать только logical IDs;
  native/provider data не попадают в client responses и безопасные ошибки.
- [x] Ограничения calls/request, concurrency, retry-next, hedge, cooldown, lease и affinity из
  [f7-09](./f7-09-bounded-provider-routing.md) подтверждены regression tests.
- [x] `gofmt`, gateway `go test ./...`, `go test -race ./...`, доступные Nix evaluation/service
  checks и code index проходят после миграции.

## Затрагиваемые файлы / слои

- [Gateway runtime](./../../../packages/llm-gateway/README.md)
- [LLM gateway Nix module](./../../../modules/llm-gateway/README.md)
- [Homelab node configuration](./../../../nodes/mytecor-homelab/README.md)
- [Nix evaluation и service tests](./../../../tests/default.nix)
- [Архитектура](./../../../ARCHITECTURE.md)

## Открытые вопросы

_нет._ Политика provider definitions, exact catalog matching, duplicate provider и fallback
зафиксирована выше; конкретная внутренняя форма compiled stage выбирается реализацией.

### Решённые при реализации

- **Catalog validation point.** Exact native validation выполняется перед dispatch в `launch`
  (и в serial fallback loop) через provider-scoped `Catalog.Validate`. Локально отклонённая
  target-пара (`model_not_found` или explicit fail-closed) не стартует upstream call и не
  расходует `max_calls`/`max_calls_per_provider`, поэтому dual-alias provider (например Hyperfusion
  с `deepseek-ai/...` в primary и `gonka/deepseek-ai/...` в fallback) всё ещё достижим в
  fallback stage при `max_calls_per_provider = 1`.
- **Stage boundary.** Первый `map` после `race` открывает fallback stage; внутри него разрешены
  только `map`, `rank` и `fallback`. `map` после declared fallback, memory stage modifiers
  (`lease`/`affinity`/`retry`/`hedge`/`semaphore`/`timeout`) в fallback stage, dangling `map` без
  `fallback`, fallback без предшествующего primary `race` и duplicate provider в одном pending
  pool отклоняются с точной ошибкой (model + rule index).
- **`retry` без `scope`** по-прежнему нормализуется в `scope = "same"`; legacy `race
  access_groups`, отдельный `models` registry и access-group routing удалены полностью.
- **Logical model registry** выводится из ключей скомпилированных планов; `/v1/models` публикует
  только unique sorted logical IDs, и неизвестный logical ID отклоняется до upstream call.
