# Мигрировать LLM Gateway на плоский routing с named routes, subroutes и «filter»

Фича: [F7 — LLM gateway](./README.md). Зависит от typed rules
[f7-11](./f7-11-typed-routing-rules.md) и от route/map контрактов
[f7-10](./f7-10-route-native-provider-mapping.md) и
[f7-09](./f7-09-bounded-provider-routing.md).

## Контекст

В `packages/llm-gateway` сейчас используется плоский список `routing_rules`, где каждый rule
содержит `match.model` и `action`. Текущая модель имеет несколько проблем:

1. `map` одновременно:
   - фильтрует providers;
   - задаёт mapping logical model → native model;
   - формирует candidate pool.
2. `retry` и `fallback` содержат собственную routing policy:
   - `on`;
   - `scope`;
   - `count`;
   - выбор следующих/same providers.
3. Fallback реализован как специальный второй stage compiler state, а не как обычный явно
   адресуемый route.
4. Нельзя явно описать отдельный route для retry/fallback и увидеть из конфигурации, какой
   именно pipeline будет выполнен при переходе.

Нужно перейти на модель: плоские `routing_rules[]` + named routes + `filter` + явные переходы
между routes. Исходный конфиг должен остаться полностью плоским — никаких вложенных `routes`,
`plans`, `children`, `fallback.plan` и подобных структур.

## Целевая модель

Каждый rule принадлежит именованному route:

```json
{
  "route": "standard",
  "action": "..."
}
```

Route — просто строковый идентификатор scope. Примеры: `standard`, `standard.retry`,
`standard.fallback`, `standard.fallback.retry`, `stupid`, `stupid.retry`. Точки в имени не имеют
специальной runtime-семантики — это только naming convention. Все rules остаются в одном
`routing_rules`.

Entry route определяется через обычные `filter`, отдельного `action = "route"` нет.

## Что сделать

- [x] 1. **Добавить «route» в каждый rule.** `route` определяет, к какому именованному pipeline
  относится rule. Rules одного route выполняются в порядке появления в глобальном
  `routing_rules`. Дублировать поля common envelope минимально, не превращая все action DTO
  обратно в один общий struct.
- [x] 2. **Реализовать typed action «filter»** как отдельный primitive с единственной
  ответственностью «ограничить применимость route или текущую selection» — без mapping,
  execution и control flow. Минимальные dimensions `where`:
  - `model`: `{"eq": "standard"}`;
  - `provider`: `{"in": [...]}`, `{"not_in": [...]}`;
  - `error`: `{"in": ["429", "5xx", "timeout", "connection_error"]}` через существующие typed
    failure classes gateway (не substring matching); минимальный набор: `model_not_found`,
    `429`, `5xx`, `timeout`, `connection_error` — переиспользовать существующую классификацию
    retry/fallback;
  - `attempt`: `{"lt": 3}` — только если естественно ложится на текущую архитектуру, не
    усложнять первую миграцию ради него.
- [x] 3. **Разделить «filter» и «map».** Убрать provider filtering из `map`. Новый `map` —
  только `{"route": ..., "action": "map", "native": "..."}`. Provider selection задаётся
  предыдущим `filter`.
- [x] 4. **Разные native model для разных provider groups.** Поддержать сценарий
  provider-a, provider-b → native-model-x; provider-c → native-model-y через последовательность
  `filter` + `map`. Ввести понятие текущей selection/cursor: `filter provider` создаёт новую
  selection от исходного доступного provider set route; `map` добавляет selection в pending
  candidate pool; следующий `filter` снова выбирает из route provider universe. Первый provider
  filter не должен навсегда удалять остальных providers из route.
- [x] 5. **Entry route через «filter».** Убрать `match` и `{"action": "route", "target": ...}`.
  Router определяет подходящий entry route из request context: routes рассматриваются
  детерминированно, порядок выводится из первого появления route в `routing_rules`, первый
  применимый entry route выбирается; если ни один не применим — явная
  routing/configuration/runtime ошибка в соответствии с существующей архитектурой.
  `match.model` после миграции не используется.
- [x] 6. **Явные subroutes через «target».** Retry, fallback и подобные control-flow actions
  ссылаются на именованный route через `target`. Пример: `{"action": "retry", "target":
  "standard.retry", "attempts": 2, "backoff": {...}}`. Destination route описывает свои
  `filter`/`map`/`race`.
- [x] 7. **Перенести retry condition из `retry.on` в target route.** Убрать из retry знания о
  retryable errors, providers, same/next, `count`, native model, race/single. Retry отвечает
  только за `attempts`, `backoff`, `target` и lifecycle повторных вызовов.
- [x] 8. **Различать route success / failure / not applicable.** Если filter destination
  subroute не проходит, это не новая ошибка: возвращается исходный terminal failure.
  «not applicable» не должен маскировать исходную terminal failure.
- [x] 9. **Переделать fallback по той же модели.** Fallback — обычный переход на named
  subroute через `target`; условия — в destination route через `filter`. Удалить специальную
  compiler semantics «second fallback stage».
- [x] 10. **Убрать `retry.scope`.** `same`/`next` больше не нужны: нужные providers явно
  фильтруются retry route, race задаётся `{"action": "race", "count": N}`. Не переносить hidden
  semantics «next unused provider» в новый `retry`.
- [x] 11. **Явная семантика used providers.** Execution context хранит providers, уже
  использованные текущим request graph; отдельная policy определяет, разрешён ли повтор
  provider. Если для первой миграции нужно сохранить текущий invariant — допустим критерий
  `{"provider": {"unused": true}}` либо эквивалентный typed condition, но это часть routing
  policy, а не неявное свойство `retry.scope`. Если можно чище — минимальный declarative
  primitive без общего expression language.
- [x] 12. **Отвязать hedge от «next retry batch».** Целевой API:
  `{"action": "hedge", "after": "3s", "target": "standard.hedge"}`, где `standard.hedge` сам
  описывает filters/providers/mapping/race/остальные execution actions. Если миграция hedge
  сильно увеличивает scope — допустимо отдельным внутренним этапом, но итоговая архитектура
  не должна оставлять hedge зависимым от старого `retry.scope/next batch`.
- [x] 13. **Сохранить «rank», «lease», «affinity», «semaphore», «timeout».** Сохранить
  существующие responsibilities и invariants: lease scoped по logical model; affinity для
  stateful Responses, known affinity fail closed; semaphore request-wide; timeout request-wide;
  одинаковые routing guarantees для streaming/non-streaming. `semaphore`/`timeout` не должны
  неожиданно reset при входе в subroute.
- [x] 14. **Компиляция в immutable граф.** Сохранить immutable `Plan` для scheduler; routing
  DTO не течёт в runtime. Схема: `routing_rules` → typed rule decode → named route compiler →
  immutable compiled routing graph → scheduler/runtime. Compiled representation — map
  `route name → immutable compiled route`; переходы retry/fallback/hedge — скомпилированные
  ссылки на target route. Target names резолвить на compile stage.
- [x] 15. **Строгая compile-time validation.** Минимально: пустой `route`; route без executable
  action; duplicate/invalid route definitions; entry route без request-level filter (если неоднозначно);
  target деps существующий (`retry`/`fallback`/`hedge` на `does.not.exist` — ошибка); routing
  cycles (`a → retry b`, `b → fallback a`) reject на compile stage (self-retry `a → retry a`
  не разрешать автоматически); filter: unknown field, unknown operator, wrong type, пустой
  `in`, unknown provider ID, unknown error class; map: без активной provider selection, пустой
  `native`, duplicate `(provider, native)`; race: без candidates, `count <= 0`, count превышает
  допустимый selection (если сейчас это config error).
- [x] 16. **Typed routing rules.** Сохранить подход из [f7-11](./f7-11-typed-routing-rules.md):
  отдельный Go type на action, strict JSON decoding, `DisallowUnknownFields`, action-specific
  validation, registry-based compiler, без центрального giant union и `switch action`. Добавить
  `rule_filter.go`, адаптировать `rule_map.go`, `rule_retry.go`, `rule_fallback.go`,
  `rule_hedge.go`. `route` вынести в общий минимальный envelope (например
  `type ruleEnvelope struct { Route string \`json:"route"\`; Action string \`json:"action"\` }`).
- [x] 17. **NixOS module.** Обновить Nix `routingRules` schema на `route`/`filter`/`target`:
  `route = "standard"; action = "filter"; where.provider.in = [...]`, `map`, `race`,
  `retry target`, `standard.retry` destination rules и т.д. Строго валидировать action-specific
  поля на Nix evaluation stage, как сейчас.
- [x] 18. **Удалить старый API.** `match`, `map.providers`, `retry.scope`, `retry.count`,
  `retry.on`, `fallback.on`, `fallbackStrategy` и прочие поля, семантика которых переехала в
  named routes/filter — удалить. При наличии старого поля config должен fail fast с понятной
  ошибкой. Compatibility aliases не делать, если усложняют implementation.
- [x] 19. **Model discovery.** Сохранить provider-scoped discovery. `GET /v1/models` возвращает
  только logical models, доступные через entry routes; subroutes (`standard.retry`,
  `standard.fallback`) не становятся логическими моделями. Logical model выводится из entry
  filter `{"where": {"model": "standard"}}` либо из compiled metadata на compile stage. Не
  раскрывать native model IDs и provider identities клиенту.
- [x] 20. **README: полностью переписать раздел Routing**, описав `route`, `filter`, `map`,
  `rank`, `lease`, `affinity`, `race`, `retry`, `fallback`, `hedge`, `semaphore`, `timeout`.
  Главная концепция: `routing_rules` — плоская упорядоченная таблица rules; `route` — named
  routing scope; `filter` — применимость/selection; `map` — mapping выбранных providers на
  native model; retry/fallback/hedge — явные переходы на другой named route. Добавить diagram
  (request → standard → retry → standard.retry; fallback → standard.fallback) и подчеркнуть,
  что физически конфигурация остаётся плоским массивом.
- [x] 21. **Тесты.** Unit и integration:
  - filter: model eq, provider in, provider not_in, error in, несколько последовательных
    filters, unknown filter field, unknown operator, invalid value type;
  - mapping: filter a,b → map X; filter c → map Y; compiled candidates a/X, b/X, c/Y;
  - entry route: model=standard → standard; model=stupid → stupid; unknown model → no
    applicable route;
  - retry: primary 429 → retry route applicable → retry call происходит; primary 400 → retry
    route not applicable → upstream retry НЕ происходит → возвращается original 400; attempts и
    backoff;
  - fallback: model_not_found → fallback route; 429 → fallback route (если filter допускает);
    400 → not applicable;
  - nested transitions `standard → standard.fallback → standard.fallback.retry`, если разрешены;
  - discovery: `standard` в `/v1/models`, `standard.retry`/`standard.fallback` нет;
  - streaming/non-streaming — одинаковые invariants.
- [x] 22. **Не делать.** Не вводить `routes: {}`, `plans: {}`, `children`, nested route objects,
  `fallback.plan`, `retry.plan`, generic goto, labels, JSONPath, jq/JS expressions, generic
  scripting DSL. Не превращать `filter` в произвольный expression evaluator — он остаётся typed
  declarative primitive. Не добавлять скрытый control flow, если его можно выразить явным
  `target`.
- [x] 23. **Не сохранять прошлую внутреннюю модель primary/fallback stage.** Текущее внутреннее
  устройство — special second fallback stage в compiler state с особым порядком actions и
  отдельной разрешительной логикой (`advance`/fallback stage в [f7-11](./f7-11-typed-routing-rules.md))
  — не переносится в новый дизайн никак, даже в скрытой или адаптированной форме. Fallback —
  это обычный явный переход на named subroute через `target` (
  `standard → standard.fallback`), как и retry; у него нет особого места в compiler state, нет
  разделения «primary stage / fallback stage», нет специального порядка действий. Все routes
  (включая fallback- и retry-subroutes) компилируются одним общим механизмом named route
  compiler; применимость fallback определяется только `filter` его destination route, а не
  положением в некой второй стадии. Валидация и тесты не должны опираться на понятие
  primary/fallback stage.

## Критерий готовности (Definition of Done)

- [x] 1. Старый `match` удалён.
- [x] 2. Каждый rule имеет `route`.
- [x] 3. Реализован typed `filter`.
- [x] 4. Provider filtering удалён из `map`; `map` занимается только native mapping.
- [x] 5. Retry использует `target`; retry conditions находятся в target route через `filter`.
- [x] 6. `scope`, `count`, `on` удалены из retry.
- [x] 7. Fallback использует `target`; fallback conditions находятся в target route; прошлая
  внутренняя модель primary/fallback stage не сохраняется ни в каком виде.
- [x] 8. Специальный fallback stage и понятие primary/fallback stage удалены из compiler state —
  fallback компилируется тем же общим именованным механизмом, что и retry.
- [x] 9. Named subroutes компилируются в immutable graph.
- [x] 10. Missing targets и cycles валидируются.
- [x] 11. `/v1/models` показывает только logical entry models.
- [x] 12. NixOS module переведён на новую schema.
- [x] 13. README полностью обновлён.
- [x] 14. Старый конфиг fail fast, а не silently мигрируется.
- [x] 15. Все существующие routing tests либо адаптированы, либо заменены.
- [x] 16. Добавлены тесты на filter, retry subroute, fallback subroute и разные native models по
  provider groups.
- [x] 17. `go test ./...` и релевантные Nix checks проходят.

## Архитектурные инварианты

- **Flat config:** `routing_rules[]` остаётся единственной структурой routing config.
- **Explicit graph:** граф строится через `route` + `target`.
- **Destination owns applicability:** не `retry says on=[429]`, а
  `retry → standard.retry`, где `standard.retry` содержит `filter error in [429]`.
- **Separation of concerns:** `filter` = selection/applicability; `map` = transformation;
  `race` = execution; `retry` = repeated transition; `fallback` = alternate transition.
- **No hidden provider policy:** retry/fallback не выбирают providers сами.
- **Immutable runtime plan:** после compilation scheduler работает только с immutable compiled
  representation.

## Затрагиваемые файлы / слои

- [Gateway runtime](./../../../packages/llm-gateway/README.md) — `rule_filter.go` (новый),
  `rule_map.go`, `rule_retry.go`, `rule_fallback.go`, `rule_hedge.go`, envelope/decoder,
  compiler, discovery, scheduler.
- [LLM gateway Nix module](./../../../modules/llm-gateway/README.md) — `routingRules` schema.
- [Nix evaluation и service tests](./../../../tests/default.nix).
- [F7 architecture](./README.md) и `README.md` gateway.

## Решённые при реализации

- **`provider.unused` реализован** как typed condition `{"where": {"provider": {"unused":
  true}}}` (filter dimension) и применяется на runtime к скомпилированному pool: execution context
  хранит providers, уже использованные текущим request graph, и unused исключает их при race
  (в т.ч. у hedge target, где фильтрация применяется в момент запуска, т.к. used set растёт во
  время гонки). Первый provider filter не удаляет остальных providers навсегда: каждый filter
  строит свежую selection из route provider universe (union providers из всех provider filters
  route), поэтому filter+map последовательности дают разные native модели per provider group.
- **`attempt` реализован** как filter dimension `{"where": {"attempt": {"lt": N}}}`: destination
  route не применяется при `attempt >= N`, попытка считается индексом входа в transition.
- **Hedge мигрирован в этой же задаче**: `{"action": "hedge", "after": "...", "target":
  "standard.hedge"}` — отдельная named route со своими filter/map/race. Hedge запускается только
  пока ветки в полёте (latency-only; быстрый terminal failure не ждёт hedge window), не клонирует
  pool и не повторяет used providers.
