# API keys как отдельное routing-действие `access` (per-route ключи, per-key models list)

Фича: [F7 — LLM gateway](./README.md). Зависит от typed rules / named routes
[f7-11](./f7-11-typed-routing-rules.md) + [f7-12](./f7-12-flat-routing-named-routes-filter.md)
и декларативного сахара [f7-14](./f7-14-declarative-models-p2c.md).

**Статус: заведена 2026-09-18, ещё не начата.**

## Контекст

Сейчас у gateway единственный глобальный ключ `client_api_key`: если он задан — все
`/v1/*` и `/admin/*` требуют один и тот же Bearer; если нет — доступ открыт (loopback).
Полноценный multi-client доступ (gateway уже служит homelab-наружу через Caddy, см.
`nodes/mytecor-homelab/config.nix`) требует другой модели:

- выдавать каждому клиенту **свой** ключ;
- **per-route** ограничивать, какими логическими моделями (entry routes) ключ может
  пользоваться;
- `/v1/models` должен отдавать **только модели, доступные предъявленному ключу**.

Требование из запроса: ключи должны быть оформлены **как отдельный рул** — то есть новое
typed routing-действие на entry route, а не ещё одно глобальное поле конфига. Это совпадает с
архитектурой: entry route уже владеет request-level свойствами (`filter where.model`,
`semaphore`, `timeout`, `continue`), и logical model registry выводится из entry routes.

## Дизайн

### 1. Go: реестр ключей `api_keys`

Топ-уровневая секция конфига — список зарегистрированных ключей-сущностей, каждому даётся
устойчивый `id` (низко-cardinality, можно логировать/не счётчик) и секрет:

```json
{
  "api_keys": [
    { "id": "pi-loopback", "key": "env.LLM_GATEWAY_KEY_PI" },
    { "id": "webui",       "key": "env.LLM_GATEWAY_KEY_WEBUI" }
  ]
}
```

- `key` разрешается как и остальные секреты — `env.ИМЯ` через `resolveConfigSecrets`
  (`config.go`); значение не попадает в public-конфиг и логи (существующий контракт
  «Prompt, body, headers и API keys никогда не логируются»).
- duplicate `id` и пустой `key` — fail fast на компляции.
- `client_api_key` остаётся «мастер-ключом» администратора с доступом ко всему (включая
  `/admin/*`) и служит обратной совместимостью: когда `api_keys` пуст, поведение
  полностью прежнее.

### 2. Go: новое routing-действие `access`

```json
{ "route": "standard", "action": "access", "keys": ["pi-loopback", "webui"] }
```

- Регистрируется в `ruleRegistry` (`rule.go`) как request-level действие на entry route —
  по духу `semaphore`/`timeout`/`continue`; новый файл `rule_access.go` (тип + `apply` +
  валидация). Канонический порядок обновляется («access первым, до filter»).
- Валидация в `apply`:
  - требуется entry route (`filter where.model` на той же route) — как `continue`;
  - `keys` — непустой non-empty список; **все id должны существовать в реестре `api_keys`**
    (напр. `ctx.errf("access references unknown api key %q", id)`, по аналогии с проверкой
    provider в `filter`) — opыбка исключена на compile stage;
  - запрещено на subroutes (у subroutes нет логической модели, auth на них бессмыслен).
- Компилируется в `compiledRoute.Access map[string]bool` (пустая = «открыта любому
  аутентифицированному ключу»). Пустой `Access` у entry route — это legacy-поведение
  (доступ по мастер-ключу / открыто при `client_api_key` = пустому).

### 3. Go: разрешение ключа и per-key models list

`Server.auth` (`server.go`) расширяется:

- Разобрать `Authorization: Bearer <key>` (subtle.ConstantTimeCompare, как сейчас).
- Мастер `client_api_key` (если задан) → идентичность «master», доступ ко всему.
- Совпадение с одним из `api_keys` → идентичность `apiKeyID`, кладётся в request context.
- Ни того, ни другого:
  - если `api_keys` непустой (ключи выдаются) и мастер не задан — ключ обязателен,
    401 `invalid_api_key`;
  - если `api_keys` пуст и мастер не задан — открыто (loopback, прежнее поведение).
- `GET /v1/models`: фильтровать `logicalIDs` по `Access` entry route модели — модель
  показывается ключу, если её entry route не имеет `access`-правила, либо включает
  предъявленный `apiKeyID`, а также мастеру.
- Inference (`chat`/`responses`): после резолва `metadata.Model`, если ключ не имеет
  доступа к модели — **404 `model_not_found`** (не раскрывать существование модели;
  совпадает с обработкой неизвестной модели и не выдаёт topology).
- `/admin/*`: только мастер-ключ (`client_api_key`); per-route ключи админ-операций не
  видят.

Высоко-cardinality `client_ip`/`api_key` по-прежнему не становятся лейблами метрик.

### 4. Nix-модуль `modules/llm-gateway`

- `types.nix`: action-подмодуль `access` (`keys = attrs/list of key ids`), `_public`
  = `{ keys = ...; }`, по образцу остальных действий — полная per-action типизация и
  re-validate на eval.
- `options.nix`: реестр `apiKeys` — attrsOf submodule (`keyFile` — runtime path agenix
  secret, `id` по умолчанию = attrname). Ключ-значения в public-конфиг не попадают:
  `api_keys` в template получает `key = null`, runtime-config builder в `config.nix`
  подставляет секреты из `LoadCredential` через jq — тем же путём, что
  `client-key`/provider keys сегодня.
- `routingRules` + генерируемый сахар: `access` можно объявлять raw-правилом на
  entry route (`standard`), что уже разрешено (raw-правила расширяют сгенерированный
  entry route — типовой случай `fallback`). При желании — sugar-опция
  `models.<name>.accessKeys` как отдельный пункт, решаемо при реализации.
- Ассерты модуля: каждая ссылка `access.keys` существует в `apiKeys`; `keyFile` требует
  включённого реестра.

## Анти-решения

- **Не** добавлять N глобальных `client_api_key`-подобных полей: ключи — сущности реестра
  с `id`, правила ссылаются на id, значение остаётся секретом.
- **Не** делать auth на subroutes (retry/fallback/hedge): доступ к модели определяется
  entry route, переходы внутри неё наследуют доступ.
- **Не** возвращать 403/логировать id при недоступной модели: 404 `model_not_found` не
  раскрывает существование модели (topology hiding).
- **Не** делать api key лейблом метрик или полем событий: только низко-cardinality `id`
  реестра, и то только если потребуется отладка — по умолчанию в логи не пишем.

## Что сделать

- [ ] 1. **Go: реестр `api_keys`** в `Config` (`config.go`), резолв секретов через
  `resolveConfigSecrets`, fail fast на duplicate/пустой id и key; `ClientAPIKey` остаётся
  мастер-ключом.
- [ ] 2. **Go: действие `access`** — `rule_access.go`, регистрация в `ruleRegistry`,
  валидация entry-only + существование id в реестре, компиляция в
  `compiledRoute.Access`.
- [ ] 3. **Go: auth/scope** — `Server.auth` (резолв ключа → `apiKeyID` в context),
  фильтрация `/v1/models` по доступу, 404 `model_not_found` для недоступных моделей,
  admin только по мастер-ключу.
- [ ] 4. **Go-тесты** (`rule_access_test.go`, `server_test.go`): единичный и множественный
  ключи, мастер+per-route, ключ с пустым `Access` видит открытые модели, чужая модель
  → 404 без dispatch, неизвестный ключ → 401, /v1/models фильтруется (включая пустой
  реестр — legacy без изменений), unknown id в `access`-правиле — compile error,
  access на subroute — compile error.
- [ ] 5. **Nix**: `types.nix` (action-подмодуль `access`), `options.nix` (`apiKeys`),
  `config.nix` (подстановка ключей через jq/LoadCredential, ассерты на ссылки и реестр).
- [ ] 6. **Тесты**: `tests/llm-gateway-bifrost.nix`/новый — template без значений ключей,
  jq-подстановка в runtime config, ассерты fail fast (unknown id, subroute access).
- [ ] 7. **Документация**: README пакета (секция API auth + action `access`),
  README модуля (опция `apiKeys`), `KEY_MANAGEMENT.md` (пример agenix-ключей).

## Критерий готовности (Definition of Done)

- [ ] 1. Каждому клиенту выдаётся свой ключ; `/v1/models` с этим ключом возвращает ровно
  присвоенные ему логические модели; запрос к другой модели — 404 `model_not_found` без
  раскрытия существования.
- [ ] 2. `access` — полноценное typed routing-действие: unknown id, запись на subroute и
  пустой `keys` fail fast на Go-compile и (для Nix-конфигурации) на eval-ассерте; JSON
  вне Nix получает ту же строгую валидацию.
- [ ] 3. `client_api_key` без `api_keys` ведёт себя ровно как раньше (обратная совместимость);
  при непустом `api_keys` и пустом мастере доступ без ключа закрыт (401).
- [ ] 4. `go test -race` чисто; `nix flake check` оценивается; тест-проверки — membership/
  инварианты, не снапшоты значений.
