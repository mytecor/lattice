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
        where = { error = { "in" = [ "429" "5xx" "timeout" "connection_error" "invalid_response" ]; }; };
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
        where = { error = { "in" = [ "model_not_found" "429" "5xx" "timeout" "connection_error" ]; }; };
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
retry/hedge → semaphore → timeout`; each entry is a discriminated rule for exactly one action and
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

`weights` задают статические веса; по умолчанию используется `priority` провайдера. `window` —
скользящее окно здоровья (по умолчанию `5m`), `errorBudget` — максимальная доля health-ошибок
(`429`, `5xx`, `timeout`, `connection_error`), до которой провайдер здоров (по умолчанию `0.2`).
Провайдер на/за бюджетом исключается из выбора `adaptive` и `round_robin`; если все кандидаты
нездоровы, выбор fail-open возвращается к базовому порядку, чтобы маршрут не отказывал.

`balance` и `lease` на одном route взаимоисключаемы (оба runtime-меняют выбор/порядок; lease
перебивает балансировку «липкостью»). Конфликт отклоняется при компиляции. `affinity`
совместим: балансировка применяется только к unpinned запросам. `balance` требует предшествующий
`map` и позицию до `race`.

Health state живёт в памяти процесса (per-provider, глобально по логическим моделям), питается в
тех же точках scheduler, где сохраняются cooldown и lease, и переживает только процесс gateway
(не перезагрузку). Это не source of truth и не credential storage.

## Logs

The gateway writes structured JSON to the systemd journal when `logLevel` is one of `error`,
`warn`, `info`, `debug`, or `trace`. The secure default is `silent`. Request bodies,
prompts, headers, and credentials are never logged. At `info`, the journal records request
start/completion with a request ID, logical model, API kind, streaming flag, status, and latency.
At `debug`, it additionally records provider routing, native model, route stage/attempt, and
latency. Upstream failures include HTTP status, error class, and a single-line truncated provider
error message.

```sh
journalctl -u llm-gateway -f -o cat
```
