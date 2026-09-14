# Балансировка провайдеров: round-robin + адаптивный выбор по здоровью

Фича: [F7 — LLM gateway](./README.md). Зависит от typed rules [f7-11](./f7-11-typed-routing-rules.md),
named routes/filter [f7-12](./f7-12-flat-routing-named-routes-filter.md) и bounded routing
[f7-09](./f7-09-bounded-provider-routing.md).

**Статус: выполнена 2026-09-14. Live-прогон на homelab закрыл DoD 1–2 и DoD 5.**

## Контекст

Живые логи gateway на `mytecor-homelab` показывают концентрацию трафика на одном провайдере:
при `priority: 100` провайдер `hyperfusion` за длинную сессию не выиграл **ни одного** запроса
(0 «accepted»), а победителями race стабильно были `gonka-proxy` (13), `gonka-openbroker` (8) и
`gonka-api` (1); `hyperfusion` в каждом запросе отменялся (499 cancelled), потому что другой
провайдер возвращал контент раньше.

Причина — не в конкретном провайдере, а в том, как устроена текущая селекция:

1. **`rank` — compile-time.** `RankRule.apply` (пакет `packages/llm-gateway/rule_rank.go`) один
   раз сортирует `ctx.st.pending` по `priority` при сборке и кладёт порядок в `compiledRoute.Pool`.
   Per-request он не работает.
2. **`race` — runtime `first-responder`.** `scheduler.go` запускает верхние `RaceCount`
   провайдеров параллельно; победитель — первый, кто вернул значимый контент (`res.winner`),
   независимо от priority. Поэтому высокий `priority` не распределяет ничего, если провайдер
   медленнее: он проигрывает latency-гонку.
3. **`lease` усиливает концентрацию.** Победитель получает аренду и промоутируется наверх
   ranking на рантайме (`applyLease`). Это «липкость к самому быстрому» — то, что сейчас и
   наблюдается.
4. **Единственный health-механизм — бинарный `cooldown`:** после retryable-ошибки провайдер
   исключается на `cooldown` (default 15s, fail-open). Ни истории производительности, ни плавной
   весовой регуляции, ни распределения «по очереди» нет.

Итог: при большом RPC вся нагрузка валится на самого быстрого провайдера, остальные простаивают
или используются только как fallback/retry-пул.

## Целевая модель

Нужны две вещи одновременно:

- **Round-robin / распределение:** при большой нагрузке запросы должны попадать на разные
  провайдеры, а не всегда на одного фаворита.
- **Адаптивная балансировка:** доля трафика провайдера должна следовать за тем, насколько он
  хорошо справляется (латентность, доля ошибок), а не быть жёстко фиксированной.

### Ключевая архитектурная тонкость

Распределение и latency-race **фундаментально в конфликте**: пока выбирает race
(first-responder), победитель всегда будет самым быстрым, какой бы порядок/вес ни задать. Чтобы
реально распределять, нужен runtime-шаг, который **выбирает одного провайдера до гонки**, и
оператор должен сконфигурить `race count: 1` (детерминированный выбор). При `race count > 1`
балансировка лишь меняет *начало* batch, но first-responder всё равно побеждает — это допустимо
как latency-hedge, но не даёт распределения.

Требуется:

- **(а) runtime-выбор** — сейчас его нет: `rank` compilation-only, `lease`/`cooldown` рантайм, но
  они не «выбирают» по весу/здоровью;
- **(б) история здоровья** — сейчас нет хранилища метрик производительности (есть только
  бинарный cooldown и lease-аренда).

## Дизайн: новый action `balance` (принятый путь)

Форма повторяет уже существующий `lease`: compile-time типизированная конфигурация на
`compiledRoute` + runtime-применение в scheduler (`applyLease` — готовый образец). Это самый
согласованный с архитектурой путь, не трогает race-инварианты и не размывает single-responsibility
компиляционного `rank`.

### 1. Runtime-состояние: `ScoreStore`

Аналог `LeaseStore` (memory-only, `sync.Mutex`), питается из scheduler-наблюдений в тех же
точках, где сейчас `r.record` / `observeLeaseWinner`:

- EWMA времени-до-значимого-контента на провайдера (для stream — first meaningful, для
  non-stream — duration);
- скользящее окно успехов/ошибок (учитывает `429`, `5xx`, `timeout`, `connection_error`);
- вывод: `health(p) ∈ [0,1]` — доля ошибок относительно `errorBudget` + латентностный фактор;
- provider с `health` ниже порога трактуется как нездоровый (дублирует/заменяет fail-open
  cooldown для этой стратегии).

Состояние per-provider (глобально), а не per-logical-model: здоровье провайдера — свойство
провайдера. (Lease остаётся per-logical-model.)

### 2. Action `balance`

Позиция в pipeline: после `filter`/`map` (и `rank` как базы весов), **до** `race`. compile-time
кладёт конфиг в `compiledRoute` (как `lease`); runtime-применение — в scheduler.

```nix
{ route = "standard"; action = "balance";
  strategy = "adaptive";           # | "round_robin" | "weighted"
  weights  = {                     # база; по умолчанию — provider.priority
    gonka-proxy = 2; gonka-openbroker = 2; hyperfusion = 1;
  };
  window       = "5m";             # окно здоровья
  errorBudget  = 0.2;              # доля ошибок, до которой провайдер здоров
}
{ route = "standard"; action = "race"; count = 1; }   # детерминированный выбор
```

Стратегии:

- `round_robin` — курсор по здоровым провайдерам (равномерно; `errorBudget`/cooldown исключает
  больных). Максимум распределения.
- `adaptive` — weighted-random по `score(p) = base(p) × health(p)`. Даёт и «разные провайдеры»,
  и сдвиг к тем, кто лучше справляется.
- `weighted` — только статические веса без адаптивности (база для тонкой ручной настройки).

### 3. Строгая валидация (в духе f7-11/f7-12)

- `balance` и `lease` на одном route — взаимоисключение: оба рантайм-меняют порядок/выбор, lease
  перебивает балансировку «липкостью». Reject на compile.
- `balance` обязательно до `race`, после `filter`/`map`.
- Если на том же route `race count > 1` — предупреждение/документация, что распределения нет
  (first-responder).
- `affinity` совместим: балансировка применяется только к unpinned запросам.
- Unknown strategy / unknown field / конфликтные ранги — fail fast, как у остальных actions.

### 4. Тесты

- Равномерность распределения на N запросах (round_robin).
- Выбивание нездорового провайдера через `errorBudget` (адаптивный вес уходит к здоровым).
- Конфликт `balance` + `lease` → compile error.
- Weighted-селекция детерминирована по стриминговой/нестриминговой ветке и data race-free
  (`go test -race`).

## Альтернативы (рассмотрено, отложено)

- **A. Расширить `rank` (`strategy: "round_robin"|"adaptive"`).** Меньше нового синтаксиса, но
  `rank` компиляционный; вносить runtime-логику в него — размыть его single-responsibility
  «упорядочить пул». Отложено в пользу отдельного `balance`.
- **C. Физически ослабить `lease`/`priority` и положиться на cooldown.** Почти без кода, но даёт
  лишь «иногда другой» без гарантии распределения и без плавной адаптивности. Отложено как
  временная палка, не решение.

## Что сделать

- [x] 1. **ScoreStore:** EWMA latency + скользящее окно ошибок; `health(p) ∈ [0,1]`; питание из
  scheduler (там же, где `r.record`/`observeLeaseWinner`); memory-only, `-race` чисто.
- [x] 2. **Action `balance` (go):** `BalanceRule` compile-time → конфиг в `compiledRoute`;
  runtime `applyBalance` в scheduler (по образцу `applyLease`); стратегии `round_robin` /
  `adaptive` / `weighted`.
- [x] 3. **Строгая валидация:** позиция до `race`; конфликт `balance`+`lease`; unknown strategy /
  field; указание про `race count > 1`.
- [x] 4. **NixOS module:** `action = "balance"` в `modules/llm-gateway/options.nix` /
  `config.nix` с `strategy`, опциональными `weights`, `window`, `errorBudget`; fail fast на Nix
  evaluation.
- [x] 5. **README (`modules/llm-gateway/README.md`):** раздел про `balance`, его позицию в
  pipeline и связку с `race count: 1`; явно — про конфликт с `lease`.
- [x] 6. **Тесты:** распределение на N запросов; выбивание нездорового; конфликт с lease;
  weighted determinism для streaming/non-streaming.
- [x] 6. **Тесты:** распределение на N запросов; выбивание нездорового; конфликт с lease;
  weighted determinism для streaming/non-streaming.
- [x] 7. **Прогон на homelab:** включить `balance` после оценки приоритетов провайдеров и
  проверки, что `hyperfusion` реально обслуживает назначенные native модели (подтверждено
  live-логами 200-ответов deepseek-ai/DeepSeek-V4-Flash-0731). Итог — round_robin на обоих
  логических моделях, `race count = 1`, lease убрана.

## Критерий готовности (Definition of Done)

- [x] 1. При большом RPC запросы распределяются между несколькими здоровыми провайдерами, а не
  только на самого быстрого.
- [x] 2. Доля трафика следует за `health` (латентность + доля ошибок), нездоровый провайдер
  выбивается.
- [x] 3. Конфиг остаётся типизированным и плоским; `balance`+`lease` на одном route fail fast.
- [x] 4. Все новые режимы покрыты Go-тестами (`-race` чисто); NixOS-валидация fail fast.
- [x] 5. README и поведение на homelab соответствуют дизайну.

## Live-прогон 2026-09-14 (закрытие DoD)

Развёрнуто через `main`/`comin` на `mytecor-homelab` (коммиты 6e72d05 → 178128d → ed1a303).
Порядок и промежуточные находки:

1. **Адаптивный с приоритет-весами концентрировал.** `balance adaptive` без явных weights
   (score = priority × health) держал ~17/18 запросов на hyperfusion: приоритет 100 против
   10–50 перебивал health, т.е. это тот же перекос, что лечим. Это ожидаемо из дизайна, но
   подтверждено live (commit 6e72d05).
2. **Адаптивный с плоскими весами всё равно концентрировал.** Даже `weights = { gonka-proxy = 2;
   gonka-openbroker = 2; hyperfusion = 1; ... }` давало ~25/27 на hyperfusion: EWMA-латентностный
   фактор (min/target) + относительная скорость доминировали над весом — adaptive спроектирован
   «сдвиг к лучшему», поэтому при быстром здоровом hyperfusion он остаётся концентратором
   (commit 178128d).
3. **round_robin распределяет primary-выбор равномерно.** Диагностический лог `balance chosen`
   (временный, убран после прогона) показал строгую ротацию по здоровым: standard 24 → hyperfusion
   8 / gonka-api 8 / gonkarouter 8; stupid 12 → hyperfusion 3 / gonka-proxy 3 / gonkarouter 2 /
   gonka-openbroker 2 / gonka-api 2. Провайдер с 429 в окне (dahl/openbroker) выбивается health-
   полом и возвращается после восстановления (commit ed1a303).
4. **Hedge возвращает кратковременную быстроту, но не ломает ротацию.** `race count = 1` гасит
   race, но `hedge after 3s` поверх него при медленном выбранном провайдере запускает быстрый
   hyperfusion и тот завершает запрос (accepted-skew в live ~2/3 на hyperfusion при выбранной
   ~1/3). Это документированный latency-hedge, а не race: primary-выбор (rotation) остаётся
   распределённым. Зафиксировано как известный трейд-офф в `modules/llm-gateway/README.md`.
5. **5xx/429 выбиваются, затем возвращаются.** Live: dahl×4 и openbroker×4 (429) уходили из
   пула на время окна, gonka-proxy с single 429 временно охлаждался — ни одного постоянного
   отказа. health-floor работает fail-open (пул пуст → базовый порядок).

Итоговая конфигурация ноды: `balance round_robin` + `race count = 1` на standard/stupid;
weights не заданы (round_robin их игнорирует), window/errorBudget — модульные дефолты 5m/0.2.

## Не делать

- Не превращать `rank` в runtime-выбрал с health-историей: это отдельный `balance`.
- Не позволять `lease` и `balance` сосуществовать на одном route (скрыто перебивают друг друга).
- Не вводить общий expression language / JSONPath / скриптовый DSL для весов — типизированные
  поля и стратегии.
- Не нарушать race-инвариант: `balance` не заменяет `race`, а управляет его выбором/порядком.
- Не реализовывать это до подтверждения, что проблемный провайдер (`hyperfusion`) вообще здоров и
  обслуживает назначенные native модели — иначе балансировка лишь размажет трафик по
  бесполезному провайдеру.
