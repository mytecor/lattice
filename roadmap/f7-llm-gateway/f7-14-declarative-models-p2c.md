# Декларативные модели и p2c-балансировка

Фича: [F7 — LLM gateway](./README.md). Зависит от балансировки провайдеров
[f7-13](./f7-13-provider-balancing.md) и typed rules / named routes
[f7-11](./f7-11-typed-routing-rules.md) + [f7-12](./f7-12-flat-routing-named-routes-filter.md).

**Статус: реализована 2026-09-16. Сахар `models`/`pipeline` + стратегия `p2c` + равные
дефолтные веса; hedge — opt-in.**

## Контекст

Две проблемы, зафиксированные по итогам f7-13:

1. **Конфиг ноды ~230 строк ручных правил.** Каждая логическая модель повторяет один и тот же
   pipeline из ~15 правил (filter model → filter provider → map → rank → balance → affinity →
   race → retry → hedge → semaphore → timeout + подроуты), `allProviders` дублируется в
   каждый фильтр, и добавление провайдера требует правки правил каждой модели. Правила —
   детерминированная функция от намерения, но пишутся руками.
2. **Автоматический балансинг без ручных весов.** Live-прогон f7-13 показал, что любые
   статические или латентностные веса концентрируют трафик: `adaptive` без явных весов
   (score = priority × health) держал ~17/18 запросов на hyperfusion, с плоскими весами —
   ~25/27 (EWMA-латентностный фактор доминирует), а приоритетные весы давали 17/18.
   `round_robin` распределяет, но требует ручного выбора стратегии и не реагирует на текущую
   нагрузку. Нужен режим, который балансирует «сам» — без per-provider весов в конфиге.

Дополнительное наблюдение f7-13: `hedge after 3s` перекашивает завершения (~2/3 завершений
уходят самому быстрому провайдеру при распределённом primary-выборе) — hedge должен быть
осознанным opt-in, а не частью генерируемого пайплайна по умолчанию.

## Дизайн

### 1. Сахар в Nix, не в Go: `models` + `pipeline`

Сахар живёт в NixOS-модуле и **генерирует канонический pipeline через тот же typed rule
evaluator** (`rewriteRule`), которым валидируются ручные правила — поэтому сгенерированный
JSON идентичен рукописному и получает всю per-action валидацию. Сахар не переносится в Go:
Go-бинарь не знает о существовании `models`/`pipeline` и по-прежнему потребляет плоский
`routing_rules`.

- `pipeline` — deployment-дефолты (каждое поле опционально): провайдеры (null = все
  включённые, выводятся из реестра по `id`), `balance.strategy` (по умолчанию `p2c`, веса по
  умолчанию равные), `raceCount = 1`, retry 2 (exponential 200ms→1s), hedge — opt-in
  (`enable`, по умолчанию false), semaphore 4/3/1, timeout 60s, affinity TTL 24h.
- `models.<name>` — `native` + per-model переопределения `pipeline` (например `raceCount = 0`
  и `semaphore.maxCalls = 6` для GLM-модели).
- Генератор (`modules/llm-gateway/config.nix`) строит для каждой модели: entry filter →
  provider filter → map → rank → balance → affinity → race → retry (+ opt-in hedge) →
  semaphore → timeout плюс `<model>.retry` / `<model>.hedge` подроуты (unused-провайдеры).
- Сгенерированные правила prepend'ятся к `routingRules`; raw-правила могут расширять
  сгенерированный entry route (типовой случай — `fallback`), но re-filter entry-модели
  запрещён ассертом модуля (второй entry route на ту же модель), raw-правила на именах
  сгенерированных подроутов запрещены, имя модели — без точек.
- Общий контракт т.: `pipeline`/`models` и `routingRules` делят одну типизированную
  машинерию через `modules/llm-gateway/types.nix`.

### 2. Стратегия `p2c` (power of two choices)

Два случайных здоровых кандидата → выбирается тот, у кого **меньше in-flight веток**:

- счётчик in-flight живёт в `ScoreStore` рядом с health-состоянием и питается в тех же
  точках scheduler, что gauge `llm_requests_in_flight` (branch launch / branch completion);
  декремент не уходит ниже нуля;
- health-пол (`errorBudget`) применяется как у round_robin/adaptive — нездоровые
  исключаются до дроу, при всех нездоровых — fail-open к базовому порядку;
- равенство in-flight сохраняет первый дров: при равном (пустом) пуле выбор равновероятен,
  при разном — балансирует очереди; латентностной обратной связи нет сознательно (f7-13:
  она и есть источник концентрации);
- детерминизм для тестов — через ту же инъекцию `pick01`, что у weighted-селекции.

### 3. Дефолт весов = равные

`baseWeight` больше не подставляет `priority` как базовый вес: без явного веса в политике
каждый провайдер несёт вес 1. `priority` остаётся только compile-time механизмом (порядок
пула для fail-open и сборки бэтчей). Меняется сигнатура `Select` — priority-closure из
`applyBalance` удалена.

### 4. Hedge — opt-in

Сгенерированный pipeline не содержит hedge, пока `hedge.enable` не выставлен явно
(deployment или per-model). Live f7-13: `hedge after 3s` давал accepted-skew ~2/3 на
самого быстрого провайдера. GLM-модель на homelab сохраняет свою топологию через
per-model override: `raceCount = 0`, `semaphore {6,4,1}`, hedge выключен.

## Анти-решения

- Не добавлять третью формулу в `adaptive` (латентностный фактор остаётся как есть):
  распределение даёт отдельная стратегия `p2c`, adaptive остаётся «сдвиг к лучшему».
- Не переносить сахар в Go-JSON: генерация правил в Nix сохраняет typed валидацию на eval
  и не добавляет второго конфиг-формата в бинарник.
- Не делать balance неявной частью race: `balance` остаётся отдельным действием с явной
  позицией и валидацией, race не меняет семантику.
- Не делать hedge по умолчанию включённым в генерируемом pipeline: live-прогон f7-13
  показал перекос завершений.

## Что сделать

- [x] 1. **Go: `p2c`** в `rule_balance.go` (валидация стратегии) + `score_store.go`
  (`selectP2C`, in-flight счётчики `IncrInFlight`/`DecrInFlight` в `ScoreStore`, питание
  из scheduler launch/runBranch рядом с метриками).
- [x] 2. **Равные дефолтные веса**: `baseWeight` возвращает 1 без явного веса; `priority`
  исключён из runtime-выбора; `Select` без base-функции.
- [x] 3. **Go-тесты**: p2c предпочитает менее загруженного (обе очередности дроу),
  нездоровые исключены, fail-open при одном здоровом, равные дефолтные веса adaptive,
  явные веса выигрывают, конкурентный inc/dec/select (`-race`).
- [x] 4. **Nix**: `types.nix` (общие submodules + `rewriteRule` + `pipelineSubmodule`),
  опции `models`/`pipeline` в `options.nix`, генератор в `config.nix` (через `rewriteRule`),
  ассерты (дубликат entry-фильтра, имена подроутов, точки в именах, валидность провайдеров).
- [x] 5. **Нода `mytecor-homelab`**: перевод `standard`/`stupid`/`smart` на сахар
  (блок llm-gateway: ~290 → ~165 строк), fallback'и остаются raw.
- [x] 6. **Тесты**: `tests/llm-gateway-sugar.nix` — генерация (p2c, retry-подроуты, hedge
  opt-in, провайдеры из реестра, per-model override, raw fallback поверх entry route,
  единственность entry-фильтра) и fail fast (дубликат entry, коллизия подроута, точка в
  имени) через `config.assertions`.
- [x] 7. **Документация**: README модуля (раздел f7-14 + p2c), README пакета (p2c, равные
  веса), tests/README.md.

## Критерий готовности (Definition of Done)

- [x] 1. Конфиг ноды описывает модели намерением (`native` + переопределения), а не ~15
  правилами на модель; добавление провайдера не требует правки правил.
- [x] 2. Балансировка по умолчанию автоматическая: `p2c` без ручных весов распределяет по
  in-flight; веса по умолчанию равные, priority не влияет на runtime-выбор.
- [x] 3. Hedge в генерируемом pipeline — opt-in (по умолчанию выключен).
- [x] 4. Все нарушения контракта fail fast на Nix evaluation (ассерты модуля); JSON
  сгенерированного pipeline идентичен рукописному (тот же typed evaluator).
- [x] 5. `go test -race` чисто; `nix flake check` оценивается; тест-проверки в стиле
  membership (регрессии контракта, не снапшоты значений).
