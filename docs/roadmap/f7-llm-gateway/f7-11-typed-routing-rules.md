# Типизировать routing actions и разнести их реализации по файлам

Фича: [F7 — LLM gateway](./README.md). Зависит от route-centric контракта
[f7-10](./f7-10-route-native-provider-mapping.md) и сохраняет bounded scheduler из
[f7-09](./f7-09-bounded-provider-routing.md).

## Контекст

Внешний `RoutingRule` сейчас является одним широким Go struct, содержащим объединение полей всех
actions: pool/race, rank, lease, affinity, retry, hedge, semaphore, timeout и legacy fallback.
Допустимые поля проверяются отдельной runtime validation, а компиляция выполняется большим
`switch action`. Nix-опция `routingRules` устроена аналогично: один submodule содержит все поля и
defaults, хотя большинство из них бессмысленны для конкретного action.

После [f7-10](./f7-10-route-native-provider-mapping.md) к pipeline добавляется `map`, а mapping и
provider selection становятся частью routing contract. Расширение общего struct ещё одним набором
optional fields увеличит число невозможных состояний и расхождение между JSON, Nix validation и
compiler semantics.

Нужно сделать action discriminator настоящей границей типов: каждый action владеет только своими
полями, самостоятельно валидируется и компилируется, а добавление нового action не требует
расширять общий union-struct или центральный switch.

## Принятое решение

### Go contract

Ввести небольшой общий rule contract и отдельные конкретные типы как минимум для актуальных после
[f7-10](./f7-10-route-native-provider-mapping.md) actions:

- `MapRule`;
- `RankRule`;
- `LeaseRule`;
- `AffinityRule`;
- `RaceRule`;
- `RetryRule`;
- `HedgeRule`;
- `FallbackRule`, если fallback остаётся самостоятельным executable stage;
- `SemaphoreRule`;
- `TimeoutRule`.

Общими остаются только действительно общие данные, например logical model selector и action
identity. Action-specific поля не должны находиться в base type.

JSON decoder сначала читает минимальный envelope с `action`, затем декодирует тот же объект в
конкретный rule type с `DisallowUnknownFields`. Неизвестный action, неизвестное поле и поле чужого
action отклоняются на decode/validation boundary. Ошибка должна содержать logical model, индекс
rule и конкретную причину без credentials или internal URLs.

Внешний config может хранить `[]Rule` через закрытый interface или эквивалентный tagged wrapper.
Compiled scheduler не должен зависеть от JSON DTO. Каждый конкретный action применяет себя к
явному compiler/builder state через небольшой общий контракт; центрального switch со всей
семантикой actions быть не должно.

### Разделение файлов

Разнести envelope/decoder, compiler state и каждый action по отдельным файлам внутри gateway
package. Ожидаемая форма — отдельные `rule_map.go`, `rule_race.go`, `rule_retry.go` и аналогичные
файлы; точные имена можно скорректировать, но нельзя снова собрать реализации в один большой файл.

Каждый action-файл содержит:

- собственный внешний тип;
- action-specific validation/default normalization;
- применение к compiler state или создание соответствующей immutable compiled policy;
- небольшие unit tests для положительных и отрицательных случаев.

Общие error-class parsing, duration и compiled-domain types остаются в общих файлах, если реально
используются несколькими actions. Выделять подпакет ценой import cycle не требуется: допустимо
сохранить `package main` и использовать файловую декомпозицию.

### Nix contract

Nix `routingRules` также должен быть discriminated, а не одним submodule со всеми defaults.
Использовать action-specific submodules/checked union либо эквивалентную строгую типизацию,
которая позволяет задать только поля выбранного action. Generated JSON должен содержать только
поля конкретного rule без неявных defaults от других actions.

Например, `RetryRule` не имеет `providers`, `native`, lease TTL или semaphore limits; `MapRule` не
имеет retry backoff; `RaceRule` не содержит mapping. Ошибка должна возникать при Nix evaluation,
если это возможно выразить надёжно, и обязательно дублироваться безопасной runtime validation для
JSON, созданного вне Nix.

### Поведенческая совместимость

Рефакторинг не меняет route semantics, принятые в
[f7-09](./f7-09-bounded-provider-routing.md) и
[f7-10](./f7-10-route-native-provider-mapping.md): candidate target mapping, ranking, bounded
batches, retry-next, hedge timing, semaphore budget, cooldown, lease, affinity, timeout,
fallback, streaming selection и cancellation должны остаться прежними.

Legacy compatibility сохраняется только в той мере, в какой она остаётся явным требованием после
[f7-10](./f7-10-route-native-provider-mapping.md). Не держать отдельный широкий legacy rule type;
если старый JSON ещё принимается, нормализовать его на входе в конкретные typed actions и покрыть
точной migration validation.

## Что сделать

- [x] Зафиксировать минимальный общий rule envelope/interface и отделить JSON DTO от compiled
  scheduler types.
- [x] Реализовать двухфазный strict decoder по `action` с `DisallowUnknownFields` для каждого
  конкретного типа.
- [x] Создать отдельный Go type и отдельный implementation file для каждого актуального action.
- [x] Перенести action-specific defaults и validation из общего compiler switch в реализации
  конкретных rules.
- [x] Заменить большой `compilePlans` switch небольшим compiler/builder contract с typed dispatch.
- [x] Сохранить индекс rule и logical model в ошибках decode, validation и compilation.
- [x] Разделить Nix `routingRules` на action-specific types и генерировать только принадлежащие
  action поля.
- [x] Удалить общий struct и мёртвые compatibility fields после миграции всех callers/tests.
- [x] Обновить developer documentation, описав добавление нового action одним типом, Nix schema,
  файлом реализации и тестом.

## Критерий готовности (Definition of Done)

- [x] В Go и Nix отсутствует единый rule type, содержащий объединение полей всех actions.
- [x] Каждому action соответствует отдельный тип и отдельный implementation file.
- [x] Добавление тестового нового action не требует изменения общего `RoutingRule` и большого
  compiler switch; меняется только discriminator registry/decoder и новый action-файл.
- [x] Unknown action, unknown field, поле чужого action, отсутствующее обязательное поле и
  неправильный порядок pipeline дают точные отрицательные тесты.
- [x] Положительные decode/compile tests существуют для `map`, `rank`, `lease`, `affinity`,
  `race`, `retry`, `hedge`, `fallback` при его наличии, `semaphore` и `timeout`.
- [x] Все scheduler, streaming, discovery, response sanitization и Nix service regression tests
  из [f7-09](./f7-09-bounded-provider-routing.md) и
  [f7-10](./f7-10-route-native-provider-mapping.md) проходят без изменения поведения.
- [x] `gofmt`, gateway `go test ./...`, `go test -race ./...`, доступные Nix checks и code index
  проходят.

## Затрагиваемые файлы / слои

- [Gateway runtime](./../../../packages/llm-gateway/README.md)
- [LLM gateway Nix module](./../../../modules/llm-gateway/README.md)
- [Nix evaluation и service tests](./../../../tests/default.nix)
- [F7 architecture](./README.md)

## Открытые вопросы

_нет_. Выбор между закрытым interface и tagged wrapper остаётся внутренней деталью при условии,
что невозможные комбинации полей не представлены публичным типом и compiler semantics разнесена
по action-файлам.

### Решённые при реализации

- **Closed interface + two-phase decoder.** `Rule` — закрытый интерфейс (`model()`, `action()`,
  `apply(*stageContext)`, unexported `isRule()`); `ruleBase` содержит только logical model
  selector и action identity, action-specific поля живут в конкретных типах. `RoutingRules`
  (`rule_decode.go`) читает минимальный envelope, выбирает тип через `ruleRegistry`
  (единственная точка регистрации action) и декодирует тот же объект с
  `DisallowUnknownFields`: unknown action / unknown field / поле чужого action отклоняются на
  decode boundary, ошибка несёт индекс rule, logical model и конкретную причину.
- **Typed compiler dispatch.** Большой `switch` в `compilePlans` удалён. Каждый action
  реализует `apply(*stageContext)`; общий compiler (`rule_compile.go`) ведёт только stage/order
  bookkeeping (`advance`: открытие fallback stage первым `map` после `race`, канонический
  порядок по rank из registry, разрешённые action внутри fallback stage) и финальную проверку
  per-model (race snapshot, dangling fallback map). Добавление action = запись в `ruleRegistry` +
  файл `rule_<action>.go` + Nix-подмодуль + тест; общего union-struct и центрального switch нет
  (`TestRuleRegistryIsTheOnlyExtensionPoint`).
- **Nix discriminated union.** `types.oneOf` из закреплённого nixpkgs не диспатчит submodule по
  discriminator (unknown-field throw внутри альтернативы не ловится), поэтому
  `lattice.llm-gateway.routingRules` реализован как checked union: значением типа `routingRule`
  является результат `lib.evalModules` над action-specific submodule, который задаёт только
  поля выбранного action (с требуемыми полями и типами). Unknown action/field/foreign field и
  missing required ловятся на Nix evaluation; `_public`-проекция генерирует в public JSON ровно
  поля конкретного action. Mytecor-homelab assertions и `llm-gateway-service` проходят
  evaluation без изменения описания правил.

### Валидация

- `gofmt`, `go vet`, `go test ./...`, `go test -race ./...` в `packages/llm-gateway` — зелёные;
- генерированный из typed Nix union конфиг mytecor-homelab проходит
  `TestValidateExternalGeneratedConfig` end-to-end (strict decoder + compile);
- проверки Nix: `.drvPath` для `mytecor-homelab`, `example` и `llm-gateway-service` eвалятся,
  регрессионные assertions по routing rules проходят.
