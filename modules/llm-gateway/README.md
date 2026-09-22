# LLM gateway module

`lattice.llm-gateway` управляет OpenAI-compatible gateway как непривилегированным systemd
service. Runtime — собственный Go proxy из `packages/llm-gateway`, использующий Bifrost Core
через Go API.

Routing и logical/native mappings являются открытой typed Nix configuration. Provider registry
описывает только transport и shared runtime identity (inference/catalog URL, Bifrost adapter,
credentials, priority, timeout, cooldown); выбор provider находится в filter action, а native
model — в action `map`, который привязывает текущую selection к одному native ID. Отдельного
списка `models` и access groups нет: logical model registry выводится runtime из entry route
filters. По умолчанию discovery URL выводится как `${inferenceUrl}/models`; `modelsUrl`
позволяет задать независимый источник. Client key, provider inference key и отдельный catalog
key поступают только через `LoadCredential`.

Модуль записывает secret-free JSON template в Nix store. `ExecStartPre` копирует его в закрытый
runtime directory и подставляет credentials через `jq`; итоговый `/run/llm-gateway/config.json`
имеет mode `0600` и исчезает при перезагрузке. Secret options — runtime path strings, не Nix paths.

```nix
{
  lattice.llm-gateway = {
    enable = true;
    logLevel = "info";
    clientCredentialFile = config.age.secrets.llm-gateway-client-key.path;

    providers = {
      proxy = {
        id = "gonka-proxy";
        inferenceUrl = "https://proxy.gonka.gg/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
      };
      openbroker = {
        id = "gonka-openbroker";
        inferenceUrl = "https://api.openbroker.gonka.gg/v1";
        modelsUrl = "https://proxy.gonka.gg/v1/models";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-openbroker.path;
        modelsApiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
      };
    };

    routingRules = let
      allProviders = [ "gonka-proxy" "gonka-openbroker" "hyperfusion" ];
    in
    [
      # Entry route for the logical model "standard": filter model selects it,
      # filter provider builds the selection, map binds one native model.
      { route = "standard"; action = "filter"; where = { model = { eq = "standard"; }; }; }
      { route = "standard"; action = "filter"; where = { provider = { "in" = allProviders; }; }; }
      { route = "standard"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
      { route = "standard"; action = "rank"; strategy = "priority"; }
      {
        route = "standard";
        action = "lease";
        source = "winner";
        duration = "10m";
        renewOnSuccess = true;
        releaseOn = [ "429" "5xx" "timeout" "connection_error" ];
        releaseAfterSlowStarts = 3;
        slowStart = "3s";
      }
      {
        route = "standard";
        action = "affinity";
        sources = [ "responses.conversation" "responses.previous_response_id" ];
        ttl = "24h";
        onMissing = "ignore";
        onProviderFailure = "fail-closed";
      }
      { route = "standard"; action = "race"; count = 2; }
      {
        route = "standard";
        action = "retry";
        target = "standard.retry";
        attempts = 2;
        backoffInitial = "200ms";
        backoffMax = "1s";
      }
      { route = "standard"; action = "hedge"; after = "3s"; target = "standard.hedge"; }
      {
        route = "standard";
        action = "semaphore";
        maxCalls = 4;
        maxInFlight = 3;
        maxCallsPerProvider = 1;
      }
      { route = "standard"; action = "timeout"; duration = "60s"; }
      # Retry subroute: applies only to the listed failures, re-selects unused
      # providers (one target per entry).
      {
        route = "standard.retry";
        action = "filter";
        where = { error = { "in" = [ "404" "model_not_found" "429" "5xx" "timeout" "connection_error" "invalid_response" ]; }; };
      }
      {
        route = "standard.retry";
        action = "filter";
        where = { provider = { "in" = allProviders; unused = true; }; };
      }
      { route = "standard.retry"; action = "map"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
      { route = "standard.retry"; action = "rank"; strategy = "priority"; }
      { route = "standard.retry"; action = "race"; count = 1; }
      # Fallback subroute: if Hyperfusion does not serve the unprefixed alias,
      # fail over to its prefixed catalog alias (model_not_found-classed local
      # rejection, never an unrelated model).
      { route = "standard"; action = "fallback"; target = "standard.fallback"; }
      {
        route = "standard.fallback";
        action = "filter";
        where = { error = { "in" = [ "404" "model_not_found" "429" "5xx" "timeout" "connection_error" "invalid_response" ]; }; };
      }
      {
        route = "standard.fallback";
        action = "filter";
        where = { provider = { "in" = [ "hyperfusion" ]; }; };
      }
      { route = "standard.fallback"; action = "map"; native = "gonka/deepseek-ai/DeepSeek-V4-Flash-0731"; }
      { route = "standard.fallback"; action = "rank"; strategy = "priority"; }
      { route = "standard.fallback"; action = "race"; count = 1; }
    ];
  };
}
```

Production configuration must provide filter/map/race rules for every advertised logical model.
Rules follow the canonical per-route order `filter → map → rank → lease | balance → affinity → race →
retry/hedge → semaphore → timeout` (the streaming `continue` takeover policy is an entry-route
post-processing action declared last); each entry is a discriminated rule for exactly one action and
owns only that action's fields. An unknown action, an unknown field, or a field owned by another
action (e.g. `providers` on `map`, `scope`/`on` on `retry`, `fallback_strategy` on `fallback`
— all removed with the old model) fails during Nix evaluation; the gateway binary independently
re-validates the generated JSON at startup, so JSON produced outside Nix receives the same strict
per-action checks. Generated JSON contains only the fields of the chosen action plus the rule
envelope `route`/`action`, never implicit defaults borrowed from other actions.

Retry, fallback and hedge own no applicability: each is an explicit transition to a named subroute
through `target`, and the destination route's own `filter` decides whether it applies. An empty
pool, a duplicate provider in one pool, a `map` without a preceding provider filter, a route
without a race, a missing target, an orphan subroute, a duplicate entry model, semaphore/timeout
on a subroute, or a routing cycle are all rejected at startup with the rule index. Legacy
`match.model`, `map.providers`, `retry.scope/count/on`, `fallback.on/fallbackStrategy`, `race
access_groups`, an independent `models` list and access-group routing are removed and fail fast;
provider transport and credentials stay in the registry.

Discovery validation is provider-scoped and exact: each target `(provider, native)` is checked
against the provider's last-known-good catalog before dispatch. Missing native → `model_not_found`
(may activate fallback), implicit catalog without snapshot → optimistic, explicit catalog without
snapshot → fail closed. No lexicographic substitution ever sends an unrelated model upstream.

The service listens on loopback by default and does not open a firewall port. Caddy remains the only
LAN ingress. The unit keeps systemd hardening enabled except for `MemoryDenyWriteExecute`: Bifrost's
Sonic/Base64x dependency loads SIMD routines with `mprotect(PROT_EXEC)` during process startup.

Responses affinity state (opaque id → provider mapping for `conversation` and
`previous_response_id`) is snapshotted to `/run/llm-gateway/affinity.json` (or `affinityFile` if
set) with mode `0600`, owned by the gateway user; it survives service restarts and is cleared on a
full service stop or reboot. The file contains no prompts, keys, or provider URLs. Failed writes
remain dirty and are retried by the next periodic/final flush; persistence errors are logged.

## Continue: внутренний takeover стрима при обрыве ответа

Action `continue` реализует in-gateway продолжение ответа, когда стрим победителя
оборвался до `finish_reason`. Проблема: апстрим может замолчать после первых чанков
(долгий GLM-reasoning, обрыв под нагрузкой) или закрыться без `finish_reason`, и клиент,
получивший начало ответа, видит `Stream ended without finish_reason`. Положиться на
клиентский retry здесь нельзя — partial-контент уже ушёл клиенту, и повторный запрос с
нуля продублировал бы его.

`continue` решает это целиком на gateway: пока стрим жив, всё как обычно (никакой
задержки). Как только победитель либо замолчал дольше `idle` после последнего события,
либо закрылся без `finish_reason` — gateway берёт весь уже отрешённый partial-вывод,
вставляет его как assistant-контекст в исходный запрос (`reshare = "full"`) и
**продолжает тот же SSE-стрим ответом другого провайдера**,
исключая сломанного. Сломанная пара (провайдер, нативная модель) уходит в cooldown/health через
те же каналы, что и обычные mid-stream-обрывы. Клиент в итоге получает полный ответ с настоящим
`[DONE]`, не видя ошибки; промежуточные partial-чанки просто продолжаются.

```nix
{
  route = "smart";
  action = "continue";
  idle = "90s";   # тишина после последнего события → триггер takeover
  reshare = "full";
  retries = 1;    # цельнопроходной re-dispatch исчерпанной цепочки
  wait = "10m";   # горизонт удержания стрима при тотальном падении пула
}
```

Поле `retries` — budget цельно-цепочного ретрая: когда **каждый** провайдер в пуле уже
оборвал стрим в ходе запроса (цепочка takeover исчерпана, `ContinueStream` не нашёл ни
одного доступного таргета), gateway раньше сразу отдавал терминальную ошибку
(`upstream stream failed`) — на практике это случается, когда gonka/hyperfusion умирают
все разом под нагрузкой. С `retries > 0` gateway вместо терминальной ошибки делает ещё
один **полный проход всей цепочки с начала**, сбрасывая множество сломанных провайдеров
(их по-прежнему держит cooldown, так что fresh-pass гонится по здоровым) и **повторно
передавая накопленный partial-вывод** как assistant-контекст — модель продолжает ответ,
а не начинает заново. Каждый ретрай тратит 1 из бюджета; когда бюджет исчерпан, а
fresh-pass снова весь оборвался, запрос завершается той же retryable ошибкой. `0`
(по умолчанию) сохраняет старое поведение — терминальная ошибка сразу после исчерпания
цепочки.

Поле `wait` — **горизонт удержания стрима** поверх исчерпанной цепочки: когда и
цельпочный ретрай не смог (весь пул провайдеров лёг разом — cooldown/health держат
каждого, доступных таргетов нет), gateway **не отдаёт терминальную ошибку сразу**, а
держит тот же SSE-стрим открытым и ждёт восстановления пула: раз в `wait`-окне он
шлёт keep-alive (`: ping`, перезапускает и клиентские, и собственный idle watchdog) и
каждые несколько секунд заново пробует `ContinueStream`. Как только провайдер
восстановился, gateway продолжает стрим его ответом (reshare partial) и завершает
настоящим `[DONE]` — клиент вообще не видит ошибки. Удержание ограничено сверху:
когда горизонт истёк, а пул так и не поднялся, терминальная ошибка отдаётся как
последний resort (клиент тогда сам решает, ретраить ли). Горизонт — явно через `wait`
(например `"10m"`); если поле опущено/`0`, удержание остаётся включённым со встроенным
горизонтом по умолчанию.

Требования и ограничения:

- Действие объявляется **только на entry-route** (там, где есть `filter where.model`):
  политика релея привязана к логической модели. В sugar включается на
  deployment-уровне через `pipeline.continue.enable` и генерится для **каждой**
  модели (per-model override через `models.<name>.pipeline.continue`).
- `idle` должен быть ≥ 5s (иначе takeover перехватывал бы честно медленные ответы),
  по умолчанию — `90s`.
- `reshare` поддерживает только `"full"`: весь полученный partial-вывод добавляется в
  history как assistant-контекст следующему провайдеру.
- `retries` неотрицателен (negative rejected) и ограничен сверху `10` (cap, чтобы
  misconfiguration не могла гонять один запрос десятками полных проходов); по умолчанию
  `0` — цепочка не ретраится целиком, терминальная ошибка отдаётся сразу после исчерпания.
- `wait` неотрицателен (negative rejected); `0` (по умолчанию) включает удержание с
  встроенным горизонтом `10m`. Удержание применяется только к исчерпанной цепочке в
  streaming chat и не срабатывает на границе tool-call (висячий tool-call нельзя переотдать
  как прозаический partial — в этом случае ошибка поверхностно-рейзится сразу).
- Работает только для streaming chat; non-stream и Responses-API не затронуты.
- Исключаются **только сломанные** провайдеры (кто реально оборвал стрим), а не все
  выбывшие из гонки: тот, кто проиграл гонку (был отменён на первом meaningful-токене
  другого), остаётся доступным для продолжения.


## Balance: распределение и адаптивный выбор провайдера

Action `balance` добавляет runtime-шаг выбора провайдера **до** `race` — то, чего не хватало для
реальной балансировки. Пока выбирает race (first-responder), победитель всегда будет самым
быстрым, какой бы порядок или вес ни задать: `rank` компиляционный, а `lease` лишь «клеит» к
самому быстрому. `balance` выбирает одного провайдера на runtime и поднимает его в начало бэтча,
поэтому **распределение достигается только при `race count = 1`** (детерминированный выбор). При
`race count > 1` балансировка лишь меняет начало бэтча, но first-responder всё равно побеждает —
это допустимо как latency-hedge, но не даёт равномерного распределения.

```nix
{
  route = "standard";
  action = "balance";
  strategy = "adaptive";
  weights = { gonka-proxy = 2; gonka-openbroker = 2; hyperfusion = 1; };
  window = "5m";
  errorBudget = 0.2;
}
{ route = "standard"; action = "race"; count = 1; }  # детерминированный выбор
```

Стратегии:

- `p2c` (по умолчанию) — power of two choices: из здоровых кандидатов случайно берутся два и
  выбирается тот, у кого меньше in-flight веток. Сигнал нагрузки — живые счётчики веток,
  питаемые scheduler в тех же точках, что gauge `llm_requests_in_flight`; раскидывает нагрузку
  при конкурентности без латентностной обратной связи (f7-13 показала, что латентностно-
  взвешенный выбор ре-концентрируется на самом быстром). При пустом (равном) пуле выбор
  равновероятен; явные `weights` смещают первый случайный дров. Скоуп сигнала:
  стримовая ветка завершается выбором победителя (первый значимый токен), поэтому
  счётчик — и gauge — покрывают фазу выбора (TTFT-окно), а не всю длину стрима.
- `round_robin` — курсор per-route вращается по здоровым кандидатам (равномерно; нездоровые
  исключаются health-порогом). Максимум распределения.
- `adaptive` — weighted-random по `score(p) = base(p) × health(p)`, где `health(p) ∈ [0,1]`
  (доля ошибок относительно `errorBudget` + относительный фактор лёгкости EWMA TTFT). Даёт и
  «разные провайдеры», и сдвиг к тем, кто лучше справляется.
- `weighted` — только статические веса, без истории здоровья (база для ручной настройки).

**Live-наблюдение (f7-13, 2026-09-14):** для распределения трафика по здоровым провайдерам на
homelab рабочей стратегией оказался `round_robin`, а не `adaptive`. `adaptive` даже с плоскими
явными весами (например `weights = { gonka-proxy = 2; gonka-openbroker = 2; hyperfusion = 1; }`)
концентрировал ~25/27 запросов на `hyperfusion`: EWMA-латентностный фактор сел на минимальную
латентность гиперфьюжена и доминировал над весом — adaptive спроектирован «сдвиг к лучшему», и
при сильно быстром здоровом провайдере он остаётся концентратором. `round_robin` (игнорирует
латентность, только health-пол) дал ровную ротацию: standard 24 → 8/8/8, stupid 12 → 3/3/2/2/2.
Выбор конкретной стратегии остаётся операционным решением: если цель — распределение, это
`round_robin`; если цель — максимизировать работу самого быстрого здорового провайдера при
fallback-запасе, это `adaptive`.

**Hedge и распределение (известный трейд-офф):** `race count = 1` убирает race, но `hedge
after 3s` на том же маршруте остаётся latency-механизмом: когда выбранный провайдер отвечает
медленнее 3s, hedge запускает быстрый альтернативный (обычно гиперфьюжен), и тот завершает
запрос. В live это давало accepted-skew ~2/3 на гиперфьюжена при выбранных ~1/3 — primary-
выбор (ротация) остаётся распределённым, а завершённость сдвигается к быстрому. Это не ломает
распределение на уровне выбора; если требуется распределение и завершённости (например при
квотировании), hedge на маршруте с `balance` нужно ослабить или убрать.

`weights` задают статические веса; **по умолчанию все провайдеры несут равный вес (1)** —
`priority` провайдера никогда не участвует в runtime-выборе (f7-13 показал, что приоритетные
весы концентрируют трафик), он задаёт только compile-time порядок пула (fail-open порядок и
порядок сборки race/hedge бэтчей). `window` — скользящее окно здоровья (по умолчанию `5m`),
`errorBudget` — максимальная доля health-ошибок (`429`, `5xx`, `timeout`, `connection_error`),
до которой провайдер здоров (по умолчанию `0.2`). Провайдер на/за бюджетом исключается из
выбора `p2c`, `adaptive` и `round_robin`; если все кандидаты нездоровы, выбор fail-open
возвращается к базовому порядку, чтобы маршрут не отказывал.

`balance` и `lease` на одном route взаимоисключаемы (оба runtime-меняют выбор/порядок; lease
перебивает балансировку «липкостью»). Конфликт отклоняется при компиляции. `affinity`
совместим: балансировка применяется только к unpinned запросам. `balance` требует предшествующий
`map` и позицию до `race`.

Health state живёт в памяти процесса (per-provider, глобально по логическим моделям), питается в
тех же точках scheduler, где сохраняются cooldown и lease, и переживает только процесс gateway
(не перезагрузку). Это не source of truth и не credential storage.

## Декларативные модели и pipeline (f7-14)

Опции `models` и `pipeline` генерируют канонический ограниченный pipeline на каждую логическую
модель прямо в routing_rules — тот же плоский JSON, что и рукописные правила, через тот же
typed rule evaluator. Это заменяет ~230 строк ручных правил на ~30 строк описания намерения;
`routingRules` остаётся escape hatch (fallback'и, native-alias'ы и всё, что сахар не выражает).

```nix
lattice.llm-gateway = {
  providers = { /* ... реестр транспортов ... */ };

  # Deployment-дефолты pipeline (каждое поле опционально):
  pipeline = {
    providers = null;          # null = все включённые провайдеры
    balance.strategy = "p2c";  # power of two choices по in-flight
    retry.attempts = 2;
    # hedge.enable = true;     # hedge — opt-in, по умолчанию выключен
    # semaphore = { maxCalls = 4; maxInFlight = 3; maxCallsPerProvider = 1; };
    # timeout.duration = "60s"; affinityTtl = "24h"; raceCount = 1;
  };

  models = {
    standard.native = "deepseek-ai/DeepSeek-V4-Flash-0731";
    smart = {
      native = "zai-org/GLM-5.3-Flash";
      # Per-provider native override: hyperfusion serves the model under its
      # own prefixed catalog alias, mapped directly in the entry pipeline
      # (not via fallback); other carriers get `native`.
      nativeByProvider = { hyperfusion = "gonka/zai-org/GLM-5.3-Flash"; };
      pipeline = {            # per-model override поверх deployment-дефолтов
        raceCount = 0;        # гонять весь пул параллельно (GLM-носители)
        semaphore.maxCalls = 6;
      };
    };
  };

  # Escape hatch: raw-правила дописываются после сгенерированных. Расширять
  # сгенерированный entry route можно (обычно `fallback`), но re-filter уже
  # объявленной модели запрещён ассертом модуля.
  routingRules = [ { route = "standard"; action = "fallback"; target = "standard.fallback"; } ];
};
```

Каждая запись `models` генерирует для одного логического модели канонический pipeline:

```text
filter (where.model) → filter provider → map → rank → balance → affinity → race
→ retry (+ opt-in hedge) → semaphore → timeout, плюс <model>.retry (и <model>.hedge,
когда hedge включён) как именованные подроуты с фильтром unused-провайдеров.
```

`models.<name>.nativeByProvider` разбивает пул по distinct native ID: для каждого
group генерируется своя пара `filter provider (in group) → map native`, так что
разные провайдеры одной модели достигают её через разные native IDs в одном stage
(без fallback). Без `nativeByProvider` это одна пара над всем пулом — прежний вывод.

Дефолты pipeline: провайдеры — все включённые (выводятся из реестра, список `id`);
`balance.strategy = "p2c"` с равными весами; `race count = 1`; retry 2 попытки (exponential,
200ms→1s); hedge выключен (opt-in); semaphore 4/3/1; timeout 60s; affinity TTL 24h. Опция
`pipeline` задаёт deployment-дефолты, `models.<name>.pipeline` — переопределения на одну
модель; итоговый выбор: модель → deployment → встроенный дефолт.

Гарантии на уровне Nix evaluation:

- пул провайдеров синхронизирован с реестром автоматически (добавили провайдера — он
  начинает обслуживать модели без правки правил);
- сгенерированные правила проходят ту же per-action типизацию, что и ручные (неизвестное
  поле, чужое поле, неизвестная стратегия — throw на eval);
- модель из `models` нельзя повторно отфильтровать в `routingRules` (второй entry route на
  ту же логическую модель) — fail fast;
- raw-правила не могут объявляться на именах сгенерированных подроутов
  (`<model>.retry` / `<model>.hedge`) — эти routes принадлежат сахару; расширять сам entry
  route можно (типовой случай — `fallback`);
- имя модели должно быть без точек: точки — конвенция именования подроутов.

## Reasoning-контроль на апстримах (strip_params / set_params)

Per-provider registry несёт две опции для управления reasoning-ключами в теле
запроса перед отправкой апстриму:

- `stripParams` (`strip_params`) — список top-level ключей, которые **вырезаются** из тела.
  Гейтвей обслуживает клиентов, кодирующих провайдер-специфичный reasoning-контроль
  (zai `thinking`), который generic OpenAI-compatible апстримы (hyperfusion/litellm)
  отклоняют с 400; strip позволяет такому провайдеру нести ту же логическую модель без
  контроля, пока другие держат нативный.
- `setParams` (`set_params`) — top-level ключи, **принудительно зафиксированные** на
  фиксированные JSON-значения. Применяется после strip, поэтому ключ в обоих списках
  в итоге получит принудительное значение, а не значения клиента. Используется чтобы
  жёстко выключить reasoning на провайдерах, принимающих контроль, например
  `thinking = { type = "disabled"; }` для zai-совместимых носителей.

Обе опции индивидуальны на провайдера: один апстрим отклоняет `thinking` (400 — только
strip), другой принимает (strip + принудительный disable). Значения `setParams` — raw JSON,
вложенные структуры проходят verbatim.

## Logs

The gateway writes one structured JSON event per line to the systemd journal when
`logLevel` is one of `error`, `warn`, `info`, `debug`, or `trace`. The secure default is
`silent`. Every line is a single JSON object carrying at least `time`, `level`, `service`
(`llm-gateway`) and `event` (a stable event name), plus the low-cardinality routing
context (`request_id`). Request bodies, prompts, headers, and credentials are never
logged.

The event hierarchy is request → attempt (see the F12 design in the gateway package
README): a normal successful request writes exactly one `request_completed` line;
transitions (retry, fallback, hedge, race, cooldown, semaphore denial) write their own
attempt-level lines only when they actually fire. A request is traceable through the
journal by its `request_id` alone: the `request_received`, `request_completed`/
`request_failed` and `llm_attempt`/`llm_retry`/`llm_fallback` lines of one request share
that id, so the retry → fallback → race path of a single request can be reconstructed
without a dedicated tracing backend.

```sh
journalctl -u llm-gateway -f -o cat
```

## Mid-stream обрывы и idle-таймаут

Планировщик выбирает winner-стрим по первому значимому событию; обрыв апстримом уже
выбранного стрима обрабатывается отдельным контуром (см. «Обрывы winner-стрима» в README
пакета): ошибка обрыва попадает в cooldown/health/lease против провайдера, а клиент
получает структурированный SSE error payload (`retryable`, `partial`, `status_code`,
`request_id`) без завершающего `[DONE]`.

`streamIdleTimeout` (по умолчанию `5m`) ограничивает полностью замолчавший winner-стрим:
если апстрим не присылает ни одного события (включая keep-alive) этот срок, стрим
отменяется, клиент получает типизированный timeout-error, а неудача записывается против
провайдера. Таймер перезапускается каждым событием, поэтому дефолт консервативен:
reasoning-модели могут легитимно паузить в середине стрима, и лимит должен превышать
любую такую паузу.

## Metrics

Gateway exports numeric metrics in Prometheus text exposition format on a dedicated
`metricsHost:metricsPort` listener (default `127.0.0.1:9209`, loopback, no `client_api_key`):

```sh
curl -s localhost:9209/metrics
```

Metrics are counters/histograms/gauges with low-cardinality labels only (`route`, `provider`,
`model`, `status`, `error_type` and the like). High-cardinality identifiers (`request_id`,
session, user, api key, client IP, prompt hash) never become labels, and the endpoint does not
require the client key (it is non-public loopback by design). Set `metricsHost`/`metricsPort`
explicitly only when a Prometheus scraper runs outside the loopback network namespace; the port
must differ from `port`.
