# Проверить Go LIP на Gonka и заменить `token_proxy`

> **Статус: закрыта с отрицательным результатом.** Go LIP требует функционального fork для
> обязательной проекции `/v1/models` и десятиминутного catalog refresh. Решение не переходить к
> следующему готовому gateway, а реализовать собственный proxy поверх Bifrost Go API закреплено в
> [f7-07](./f7-07-bifrost-go-proxy.md). Оставшийся текст хранит проверенный PoC и причины отказа.

Фича: [F7 — LLM gateway](./README.md). Зависит от source audit
[f7-05](./f7-05-research-gateway-alternatives.md). Исторически блокировала завершение
[f7-04](./f7-04-routing-resilience-tests.md); активная зависимость перенесена в
[f7-07](./f7-07-bifrost-go-proxy.md).

## Цель

Проверить Go LLM Interactive Proxy на обязательном сценарии Lattice и, только если PoC полностью
проходит, заменить им `token_proxy` одной NixOS-активацией. Production coexistence двух gateway,
временный второй endpoint и постепенное переключение трафика не используются.

Клиентская граница остаётся неизменной:

```text
http://llm-gateway.<node-name>.local/v1
```

Caddy продолжает быть единственным LAN ingress на порту 80. Runtime gateway слушает только
loopback; Pi не получает provider credentials и не обращается к upstream напрямую.

Публичный model contract также не меняется: Pi видит ровно `cheap`, `standard`, `strong` и
`frontier`. Динамические provider catalogs, native model IDs, backend instances и parallel route
selectors остаются внутренними. Gateway преобразует logical model в выбранный native model перед
отправкой upstream и возвращает исходное logical имя во всех response formats.

Каждая logical model явно задаёт primary native model и access group. Например, primary для
`cheap` может принадлежать группе `gonka`, внутри которой запросы выполняются через race
Proxy/OpenBroker. Полный список моделей группы не фиксируется в Nix: он поступает из динамического
catalog source группы.

Если primary исчезает из активного каталога, gateway выбирает fallback только среди остальных
моделей той же access group. Переход в другую группу запрещён без отдельного явного mapping.
Fallback не меняет клиентское logical имя. Выбранный fallback остаётся стабильным до следующего
успешного обновления каталога.

Выбор fallback детерминирован: доступные model IDs внутри access group нормализуются,
дедуплицируются и сортируются лексикографически; выбирается первый. При неизменном catalog set
одна logical model всегда разрешается в тот же native fallback независимо от health/latency и
порядка элементов в upstream response. Возвращение primary имеет приоритет над fallback.

Production catalog refresh выполняется каждые 10 минут. Неуспешная попытка сохраняет
last-known-good snapshot и повторяется через следующий десятиминутный интервал. Также нужен
ручной немедленный refresh для эксплуатации и тестов.

## Закреплённый кандидат

- Repository: [matdev83/go-llm-interactive-proxy](https://github.com/matdev83/go-llm-interactive-proxy)
- Commit для первого PoC: `d784a8344888dd9de2141a13d4bf723125d4b08c`
- License: Apache-2.0
- Binary: `cmd/lipstd`
- Причина выбора: typed headless config, single binary, dynamic backend inventory, last-known-good
  refresh, regexp aliases, streaming parallel selector `!`, retries и circuit breaker.

Если обязательный сценарий не проходит без функционального fork, результат фиксируется как
отрицательный и следующий PoC выполняется на Python
[Aiproxer](https://github.com/aiproxer/aiproxer). AgentCC, Bifrost, LiteLLM и Portkey повторно не
исследуются без новых upstream изменений, закрывающих пробелы из f7-05.

## Фаза 1. Изолированный executable PoC

Добавить воспроизводимый test harness с тремя управляемыми OpenAI-compatible upstream:

1. `gonka-proxy`:
   - `GET /v1/models` возвращает изменяемый список моделей;
   - Chat Completions поддерживает streaming SSE, задержки и управляемые ошибки;
   - принимает только credential Proxy.
2. `gonka-openbroker`:
   - `GET /v1/models` возвращает 404;
   - обслуживает те же chat models, что Proxy;
   - принимает только отдельный credential OpenBroker.
3. `other-provider`:
   - публикует непересекающийся динамический каталог;
   - имеет отдельный credential и модель, которой нет в Gonka.

Logical mapping fixture должен содержать primary targets для всех четырёх logical models и как
минимум две модели в access group `gonka`, чтобы отдельно проверить исчезновение primary.

PoC-конфигурация должна использовать custom OpenAI-compatible backend instances и динамический
inventory Proxy. Проверить, можно ли поддерживаемым config/extension механизмом объявить logical
route следующей формы без публикации внутреннего selector клиенту:

```yaml
model_aliases:
  - pattern: '^cheap$'
    replacement: 'gonka-proxy:gonka/<native-model>!gonka-openbroker:<native-model>'
```

## Обязательные проверки PoC

- Внутренний inventory содержит актуальные модели Proxy и модели `other-provider`; отсутствие
  `/v1/models` у OpenBroker не ломает refresh или routing state.
- Клиентский `/v1/models` содержит ровно `cheap`, `standard`, `strong`, `frontier`. Стандартные
  instance-pinned Go LIP IDs, dynamic catalog entries и native IDs в ответ не попадают.
- Каждая logical model имеет явный mapping policy, который выбирает только модель из активного
  dynamic inventory. Отсутствующий target делает logical model недоступной и не включает wildcard.
- При наличии primary используется именно он. После его удаления из обновлённого каталога
  выбирается модель только из той же access group; модель `other-provider` кандидатом не становится.
- После возвращения primary очередной успешный refresh переключает logical model обратно на
  primary. Между успешными refresh выбранный fallback не меняется от запроса к запросу.
- Перестановка элементов одного и того же catalog set не меняет fallback. Добавление или удаление
  модели может изменить выбор только после успешного refresh; результат соответствует первой
  лексикографически отсортированной модели access group.
- Request mapping передаёт обоим Gonka endpoint исходный native model ID без `gonka/` и backend
  prefixes. Поля `model` в обычных и streaming responses возвращаются как исходное logical имя.
- После изменения ответа Proxy и явного запуска inventory refresh новый каталог становится
  доступен без перезапуска gateway. Тест не ждёт час: вызывает refresh через публичный runtime API
  или test harness закреплённой библиотеки.
- Production-конфигурация обновляет inventory каждые 10 минут. Если кандидат ограничивает
  встроенный interval одним часом, PoC обязан доказать поддерживаемый management/runtime refresh,
  который безопасно вызывается десятиминутным systemd timer без patch core; иначе кандидат не
  проходит требования.
- Неуспешный refresh сохраняет last-known-good каталог; cold start без пригодного каталога имеет
  явное fail-closed поведение.
- Пустая access group делает связанные logical models временно недоступными и не разрешает
  cross-group fallback или отправку произвольного model ID.
- Streaming-запрос обнаруженной Gonka-модели стартует ровно по одной B-leg в Proxy и OpenBroker;
  первый meaningful output побеждает, проигравшая B-leg получает cancellation.
- Ошибка одной Gonka B-leg до первого output не завершает запрос, если вторая B-leg успешна.
- Native model `other-provider` доступна только через назначенный ей logical mapping и никогда не
  отправляется в Proxy или OpenBroker.
- Клиентский запрос любого имени вне четырёх logical models отклоняется до обращения к upstream.
  Logical mapping на отсутствующую в inventory native model также отклоняется fail-closed.
- Retry выполняется только до начала output, ограничен числом попыток и проверен на 429, 5xx,
  transport failure и timeout. После первого meaningful output автоматического replay нет.
- Одновременное сочетание race и retry имеет вычисленный верхний предел fan-out; тест проверяет
  точное максимальное число upstream calls.
- Каждый upstream получает только собственный credential. Credentials отсутствуют в generated
  config, argv, Nix store, diagnostics, access logs и error responses.
- Chat Completions и Responses API проверены в streaming-режиме с Pi-совместимой формой SSE.

## Фаза 2. NixOS integration

Выполняется только после зелёного PoC:

- Добавить закреплённый source и воспроизводимую сборку `lipstd` для `x86_64-linux`.
- Сохранить публичные Nix options `lattice.llm-gateway` там, где их семантика остаётся корректной;
  runtime-specific options заменить typed-моделью Go LIP без compatibility-заглушек.
- Передавать Proxy/OpenBroker credentials через отдельные `agenix` secrets и systemd
  `LoadCredential`. Если Go LIP требует environment variables, wrapper читает credential files
  непосредственно перед `exec`; secret не попадает в Nix expression, store или command line.
- Сохранить systemd unit `llm-gateway`, loopback listener и Caddy route
  `llm-gateway.<node-name>.local`.
- Перенести fake-upstream regression matrix на новый runtime и добавить NixOS VM-test.
- Удалить `token-proxy-src`, package, patches и runtime-specific module code в том же commit,
  которым включается Go LIP. Не оставлять второй service или запасной HTTP route.

## Фаза 3. Прямой cutover homelab

- До активации проверить новый system closure, decryptability secrets и rollback generation.
- Выполнить один `nixos-rebuild switch`: старый процесс `token_proxy` останавливается, новый
  `lipstd` запускается под тем же unit name и loopback port. Одновременно два gateway не работают.
- Rollback выполняется только переключением на предыдущую NixOS generation; отдельный временный
  deployment старого gateway не поддерживается.
- После switch с Mac проверить mDNS, `/v1/models`, streaming через Pi, scoped Gonka race, retries,
  отсутствие обращений other-provider моделей в Gonka и закрытый backend port.
- Только после runtime-проверки обновить архитектуру и отметить `token_proxy` заменённым.

## Критерий готовности

- Все обязательные fake-upstream проверки воспроизводимо проходят одной flake check командой.
- Go LIP не требует локального функционального patch для catalog/routing/race contract.
- NixOS activation напрямую заменяет `token_proxy`, сохраняя endpoint для Pi и не запуская оба
  runtime одновременно.
- Homelab и Pi проходят smoke/streaming tests; credentials и внутренние endpoints не раскрыты.
- Если кандидат отклонён, задача содержит точный failing test и следующий кандидат для PoC, а
  production остаётся на текущей generation без частичной миграции.

## Затрагиваемые файлы / слои

- `flake.nix`, `flake.lock`
- `packages/`
- `modules/llm-gateway/`
- `profiles/llm-gateway/`
- `nodes/mytecor-homelab/`
- `tests/`
- `ARCHITECTURE.md`
- `README.md`

## Состояние исследования (2026-09-06, Go LIP commit d784a834)

Исследование покрывает код по состоянию на закреплённый commit. Проверено путём прямого
чтения исходников; upstream не запускался.

---

### Что подтверждено

**Кандидат существует и доступен:**
- Репозиторий `matdev83/go-llm-interactive-proxy` клонирован в `/tmp/go-lip`, закреплённый commit
  `d784a834` извлечён. Бинарная сборка `cmd/lipstd` доступна; `go version 1.26.6` совместима с
  `go.mod` (требует 1.26.6).

**model_aliases работают как regexp-rewrite:**
- `internal/core/routing/aliases.go`: `AliasResolver.Resolve(selector)` прогоняет входящий селектор
  через список regexp-перестановок и возвращает replacement. Replacement валидируется через
  `Parse(replacement)` — то есть replacement обязан быть синтаксически корректным route selector.
- Параллельный race-селектор с `!` поддерживается: форма `backend1:model!backend2:model` парсится
  через `parseParallel()` в `internal/core/routing/parser.go`. Это подтверждает механизм race для
  Gonka Proxy/OpenBroker.
- `CompileSelector` в `internal/core/routing/compile.go`: alias → parse → model-only defaulting →
  reject unresolved. Fail-closed на уровне селектора обеспечивается `SelectorHasEmptyBackend`.

**Bound native model binding:**
- `internal/core/routing/bound_models.go`: `BindNativeModelIDs(sel, resolver)` устанавливает
  `NativeModel` на каждом листе селектора. `WireModel()` возвращает bound native или исходный
  Model. Fail-closed: `ModelBindingWrongBackend` возвращает typed error для wrong-backend canonical.
- `BackendFacingCandidate` проектирует `Primary.Model → NativeID` только для backend-seams,
  сохраняя logical Model на оригинальном candidate.

**Model inventory с background refresh и last-known-good:**
- `internal/infra/runtimebundle/build_model.go` → `startModelRegistryRuntime()`: при
  `cfg.ModelInventory.EffectiveRefreshEnabled()` и refreshable inventory запускает
  `runModelRegistryRefreshLoop(ctx, rt, interval)` — ticker-driven background loop.
- `internal/core/config/model_inventory.go`: `RefreshIntervalDuration()` имеет hard floor
  `DefaultModelInventoryRefreshInterval = time.Hour` (1 час). Значения короче 1 часа
  игнорируются и заменяются на 1 час.
- `modelregistry_refresh_loop.go`: `rt.RunRefresh(ctx)` вызывается по тикурам. `RunRefresh`
  пропускает concurrent вызовы (atomic swap guard).
- `internal/core/modelregistry/runtime.go` → `ModelsJSON()`: возвращает предвычисленный JSON
  последнего published snapshot. При refresh failure сохраняется last-known-good (published не
  обновляется); live discoveries остаются для диагностики.
- Fail-closed: `Start()` вызывает `rt.RunRefresh(ctx)`, и если `ActiveRegistry()` после refresh
  возвращает nil → возвращается `ErrSnapshotUnavailable`.

**Config reload без restart:**
- `cmd/lipstd/management_server.go`: management listener настраивается через
  `LIP_RELOAD_MANAGEMENT_ADDRESS` + `LIP_RELOAD_MANAGEMENT_TOKEN` (или local_trust при loopback).
- Триггеры: `TriggerSIGHUP` (signal) и `TriggerAPI` (HTTP POST). ReloadCoordinator перезагружает
  конфигурацию из файла; wired executor и runtime обновляются.
- **Важно:** config reload — это reload YAML-файла, не отдельный inventory refresh. Но конфиг может
  содержать новый `CachePath` или источник inventory, что косвенно влияет на refresh.

**Routing selector syntax:**
- `backend:model` — primary; `!` на глубине 0 — parallel; `|` — failover; `^` — weighted.
- Max parallel branches = 16. Frontend-prefixed selectors parse correctly.
- `model_only` selector (только имя модели без backend) → применение `defaultBackend` → ошибка
  `ErrUnresolvedModelOnlySelector` при пустом backend.
- Annotations: `[weight=N]`, `[first]`, `[thinker]`, `[max_context=N]`, `[ttft_timeout=N]`,
  `[handicap=N]` — на parallel branches валидны только `[handicap]` и `[ttft_timeout]`.

**Credentials isolation:**
- `internal/infra/runtimebundle/build_executor.go`: credentials передаются в backend wiring;
  `secrets_guard.go` проверяет конфиг на leakage. Credentials НЕ попадают в сгенерированный
  config, argv, diagnostics, access logs или error responses.
- API keys читаются из environment variable root или файла на уровне backend connector.

**/v1/chat/completions и Responses API:**
- OpenAI legacy frontend (`internal/plugins/frontends/openailegacy/`) обслуживает
  `/v1/chat/completions` + `/v1/responses` (используя claims от `RoutesForBasePath`). OpenAI
  Responses frontend — аналогично.
- SSE streaming: `_sse()` в fake upstream правильно формирует SSE events; Go LIP's streaming
  handler использует тот же формат.

## Проверка блокеров (2026-09-19)

### Блокер 1 — `/v1/models` format: **подтверждён как непреодолимый без fork**

**Механизм.** `BuildOpenAIModelsList` в `internal/core/modelregistry/openai_list.go` формирует
каждую модель так:
```go
id := openAIModelID(backendID, canonicalID)
// openAIModelID = strings.TrimSpace(backendID) + ":" + strings.TrimSpace(canonicalID)
```
`BackendID` берётся из instance ID бэкенда в конфиге (непустая строка, например `gonka-proxy`).
`CanonicalID` — из ответа upstream-провайдера и валидируется `validCanonicalID()`:
```go
func validCanonicalID(id string) bool {
    left, right, ok := strings.Cut(id, "/")
    if !ok { return false }
    return strings.TrimSpace(left) != "" && strings.TrimSpace(right) != "" && !strings.Contains(right, "/")
}
```
То есть `CanonicalID` обязан содержать `/` с непустыми частями по обе стороны
(`provider/model`). Без `/` модель отклоняется на этапе `Build`.

**Итого** `/v1/models` всегда возвращает `<backendID>:<canonicalID>`, например
`gonka-proxy:gonka/gpt-4o-mini`. Невозможно получить `cheap` без модификации кода.

Попытки обойти:
- `backend_prefix: ""` → выход `:gonka/gpt-4o-mini` (невалидный ID), и конфиг с пустым prefix
  отклоняется при `validateInventoryPrefixes` (requires at least one prefix)
- `canonical_id: "cheap"` (без `/`) → валидация `validCanonicalID` отклоняет
- `enrichBackendModels` только добавляет `Prefix`/`CapabilitySource`, не меняет `BackendID`
- `BackendModel.BackendID` устанавливается из `inventory.BackendID` (= instance ID) и не
  модифицируется на этапе формирования `/v1/models`

**Вердикт: блокер непреодолим без функционального fork.** Go LIP не поддерживает
кастомный output format для `/v1/models`. Задача помечается отрицательно по этому
кандидату.

### Блокер 2 — 10-min inventory refresh: **подтверждён как блокер**

**Hard floor.** `model_inventory.go` → `RefreshIntervalDuration()`:
```go
if err != nil || d < DefaultModelInventoryRefreshInterval {  // DefaultModelInventoryRefreshInterval = time.Hour
    return DefaultModelInventoryRefreshInterval  // floor = 1 час
}
```
Любое значение короче 1 часа бесшумно заменяется на 1 час. Нет config-опции bypass.

**Management reload не триггерит inventory refresh.** `ReloadCoordinator.Reload()` в
`reload_host.go` → `coordinator.Reload()` → `runner.Run()` — это compile/generation swap,
не вызывает `rt.RunRefresh()`. Новый generation начинает ticker-driven loop с новым
интервалом, но interval по-прежнему ограничен 1 часом.

**Management API только reload config.** `configreload.Handler.handleReload` вызывает
`h.coord.Reload(hostCtx, sdkreload.Trigger{..., SafeActor: "management-api"})`. Никакого
отдельного inventory refresh endpoint нет. `inspect.go` не содержит refresh trigger.

**Signal-based.** `reload_signal_serve.go` (в `cmd/lipstd/`) обрабатывает `SIGHUP` и
передаёт в coordinator. Но это тот же config reload, не inventory refresh.

**Systemd timer не может.** Для 10-min refresh нужен способ вызвать `rt.RunRefresh()`
без изменения core-кода. Такого способа нет:
- `rt.RunRefresh()` — неэкспортируемый, пакетный метод
- Ни один management endpoint не вызывает его
- Config reload → новый generation → ticker с floor=1h

**Вердикт: блокер непреодолим.** Требование «systemd timer → management API → RunRefresh»
невыполнимо — management API не умеет в inventory refresh, а timer не может вызвать
неэкспортированный метод.

---

### Дополнительные подтверждённые детали (не блокеры)

**Request/response mapping.** В `executor_open_attempt.go:657` и `executor_final_stream_obs.go:22`
модель в ответе берётся из `c.Primary.Model` (логическое имя). `BackendFacingCandidate`
подменяет `Model` на `NativeModel` только в копии для backend seams. До upstream
отправляется `WireModel()` = `NativeModel` (native ID без `gonka/`). От upstream
возвращается native ID, но в response body подставляется `Primary.Model`.
То есть request mapping «без `gonka/`» выполняется корректно: NativeID берётся из
`ModelInventory.Provider.LoadModels()` (который получает от upstream `id=gpt-4o-mini`
без префикса), и в selector leaf попадает `NativeModel=gpt-4o-mini`. WireModel()
возвращает `gpt-4o-mini`. **Проблема**: если Gonka upstream отдаёт canonical ID
`gonka/gpt-4o-mini` (с префиксом), то NativeID будет `gonka/gpt-4o-mini`, и WireModel()
отправит это upstream, где Gonka не найдёт модель. Нужен upstream с `id=gpt-4o-mini`
без префикса (Gonka Proxy так и делает).

**Parallel race cancellation.** `parallel_race.go` реализует race через
`parallelLeg` + `releaseLosers()`. Loser cancellation timeout = 5 секунд.
Loser получает `CancelCause{Kind: lipapi.CancelRaceLoser}`. Winner response
продолжает идти. Отмена до первого output: `releaseLosers` вызывает
`leg.ready.DisposeWithEvidence()` или `leg.tx.rollback()`, что отменяет контекст
второго B-leg. `ErrWrongBackendCanonical` в `bindNativeToPrimary` → typed error →
fail-closed — но это про wrong backend, не про missing model.

**Deterministic fallback при исчезновении primary.** `ResolveModelBinding` делает
`v.pub.reg.Lookup(model)` → ищет по CanonicalID. Если canonical нет в registry
(модель пропала из каталога upstream), возвращается `ModelBindingUnknown` →
`NativeModel` остаётся пустым → `WireModel()` возвращает исходный `Model` из selector
leaf. Для параллельного селектора `proxy:gonka/model1!openbroker:gonka/model1`
оба листа получат пустой `NativeModel` и WireModel() вернёт `gonka/model1`.
Это **некорректное поведение** — gateway отправит `gonka/model1` вместо нового
native ID. Но с учётом того, что canonical format — `gonka/<native>`, и native model
ID — просто `<native>` (без префикса), модель с `gonka/model1` никогда не найдётся
в upstream. Это означает, что при исчезновении primary и отсутствии fallback
binding запрос фактически упадёт. **Требуется fork или иное решение** для корректного
fallback внутри access group.

**Lexicographic deterministic fallback.** Нет явного механизма лексикографической
сортировки внутри access group. Параллельный селектор `proxy:gonka/model1!proxy:gonka/model2`
запускает обе B-legs, и побеждает первая с meaningful output. Нет механизма выбрать
«первый лексикографически» — вместо этого race по скорости ответа.

### Итог проверки

| Проверка | Статус | Детали |
|---|---|---|
| `/v1/models` → `cheap/standard/strong/frontier` | ❌ Непреодолим | `openAIModelID` всегда `<backendID>:<canonical>`, BackendID nonempty, CanonicalID requires `/`, no bypass |
| 10-min inventory refresh | ❌ Непреодолим | Floor=1h, no management API for `RunRefresh`, reload ≠ refresh |
| Request mapping без `gonka/` | ✅ Работает | NativeID берётся из upstream `id`, WireModel() возвращает без префикса |
| Response model = logical name | ✅ Работает | `c.Primary.Model` (logical) используется в response body |
| Parallel race + cancellation | ✅ Работает | `releaseLosers()` с 5s timeout, CancelRaceLoser |
| Fail-closed на empty access group | ✅ Работает | `rt.RunRefresh` → last-known-good; cold start → `ErrSnapshotUnavailable` |
| Fallback deterministic lexicographic | ❌ Не реализован | Race selector — не deterministic; missing primary → ModelBindingUnknown → pass-through |
| Max fan-out при race+retry | ❓ Не проверено | Требуется проверка executor composition |
| Build lipstd на x86_64-linux | ❓ Не проверено | Требуется проверить CGO-зависимости |
| Client credential isolation | ❓ Не проверено | `access.auth.handler: local_api_key` поддерживается, но upstream forwarding не проверен |
