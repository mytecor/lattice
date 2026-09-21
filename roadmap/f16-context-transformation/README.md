# F16. Context transformation в Lattice gateway

**Статус:** план от 2026-09-21; runtime ещё не реализован. Зависит от
[F7](./../f7-llm-gateway/README.md), задачи [f16-01](./f16-01-context-contract-budget.md)–[f16-09](./f16-09-context-acceptance.md).
Это встроенная возможность существующего Go gateway, а не новый proxy или сервис.

Соответствует [вехе F16](./../../ROADMAP.md#f16-context-transformation).

**Критерий готовности:** gateway преобразует полную Chat Completions history в валидный
provider-facing context в пределах budget, сохраняет exact recent tail и переиспользует
cached representations неизменившихся segments; интеграция и opt-in runtime acceptance
подтверждены в [f16-09](./f16-09-context-acceptance.md).

**Осознанно откладываем:** embeddings/hybrid retrieval, hierarchical summaries, detailed/raw
promotion, semantic prefetch и diff-based tool outputs — после MVP.

## Граница интеграции

Сейчас [HTTP facade](./../../packages/llm-gateway/server.go) создаёт `ExecuteRequest{Kind, Body}`
и передаёт его [Runner](./../../packages/llm-gateway/router.go). Routing выбирает provider/native
model; [Bifrost executor](./../../packages/llm-gateway/bifrost_executor.go) выполняет
`rewriteRequestBody` (model, strip/set params) и отправляет запрос. Pipeline встраивается
после authentication, проверки JSON и logical model, до `RunWithResult`/`SelectStream`.
Будущий route-level `access` из [f7-15](./../f7-llm-gateway/f7-15-api-keys-access-rule.md) должен проверяться
до платной summarization; F7-15 не является зависимостью MVP с единым client key.

```text
full OpenAI messages[] (immutable source)
  -> accounting + canonical turns + protected tail
  -> tool-output-compress (old tool results only)
  -> history-compact (stable segments + cached summary/synopsis)
  -> context-select (query-dependent BM25 + budgeted assembly)
  -> ordinary OpenAI messages[]
  -> existing Runner: map/balance/retry/fallback/race/hedge
  -> existing Bifrost executor -> provider
```

Это три отдельных Go-компонента с явными входами/выходами, cancellation и ошибками.
Подготовка turns/budget — общая механика, не четвёртый semantic transformer.
Предлагаемые типы: `SourceHistory`, `CanonicalTurn`, `Segment`, `Representation`,
`BudgetPolicy`, `TransformResult`; названия уточняются в реализации.
Raw bytes/messages хранятся отдельно от производных, source ranges позволяют найти оригинал
любого segment. Входной body не мутируется. Переписывается только `messages`, остальные
поля и неизвестные расширения сохраняются. Recent messages сохраняются verbatim, включая
content blocks, tool arguments, IDs и provider extensions.

MVP обрабатывает только `/v1/chat/completions` с полной `messages[]` (stream и non-stream).
`/v1/responses`, `previous_response_id` и существующая Responses affinity обходят pipeline:
из opaque server-side state полную историю восстановить нельзя. `tool_result` в чужом
протоколе не считается автоматически OpenAI `tool`: неизвестные формы не преобразуются.
Multimodal/opaque/signed blocks не превращаются в текст и не исчезают: поддержанный estimator
учитывает их стоимость, иначе pipeline пропускает transformation с явной причиной.
Для включённой политики неизвестный размер/лимит не даёт права отправлять заведомо oversized request.

Source of truth — только полная история данного входного запроса. Gateway не создаёт session
lifecycle и не обещает восстановить историю, которую клиент уже удалил. Для Pi отдельно
проверяется клиентская auto-compaction: увеличивать рекламируемое окно без полноценной передачи
истории нельзя. Cache — disposable derivative, не session store и не замена Responses affinity.

## Budget до выбора provider

Нынешний [catalog](./../../packages/llm-gateway/catalog.go) хранит IDs моделей, а не достоверные
context limits. Нужен явный capability contract для каждого достижимого provider/native target,
включая retry, fallback, hedge и continue. Не определять окно по имени `standard`/`stupid`/`smart`.
Для MVP используем консервативный минимум доступного input budget по всем таким targets;
не пересчитываем его по текущему cooldown или winner. Неизвестный limit при включении
политики требует явного override либо исключения target из её допустимого пула.

Для каждого target: `B = context_window - output_reserve - safety_margin`.
Output reserve учитывает `max_completion_tokens`/`max_tokens`, default при их отсутствии,
provider output cap и изменения `strip_params`/`set_params`. Несовместимые значения валидируются.
Input cost включает messages, роли, tool calls, `tools`/schemas, response format и прочий
provider-visible overhead; estimator имеет ID/version, margin и признак exact/estimated.
Для нескольких tokenizers проверяем лимит каждого target, а не только минимум числовых окон.

Начальные настраиваемые значения: passthrough при `input < 0.70 * B`, цель после сжатия
`0.60 * B` (допустимый ориентир 55–65%), hard limit `B`. Protected tail — до 6 последних
logical turns и до 25% B; добавляем turns с конца целиком. Текущий turn обязателен даже
при превышении tail quota. System/developer instructions сохраняются отдельно, не суммаризируются
и не повышают привилегии generated text; порядок и область действия поздних инструкций требуют
сохранения, при невозможности безопасной сборки — bypass/error.
Если обязательная часть больше target, цель ослабляется до hard limit; если больше hard limit,
возвращается OpenAI-compatible `context_length_exceeded` до upstream, без обрезания user request
или tool chain. Проверка итогового body обязательна даже после успешного сжатия.

После tool compression повторно считаем размер. Если достигнута цель, semantic LLM-вызов
не нужен. Если pipeline отключён, сохраняется текущий gateway contract.
При ошибке стадии разрешён оригинал или проверенный промежуточный результат только если он
помещается в hard budget; иначе явная ошибка, а не silent truncation или oversized fail-open.

## Turns, segments и reusable representations

Logical turn начинается с user request и включает assistant tool calls, все связанные results
и assistant continuation до следующего user turn. Несколько tool rounds и parallel calls
остаются атомарными. Незавершённая цепочка относится к protected tail; malformed/orphan IDs
выявляются до transformation и не «исправляются» придуманными сообщениями.

Segmentation выполняется по исходным canonical turns, независимо от запроса retrieval и длины
recent tail. Детерминированный проход от начала закрывает page на завершённом turn с task/topic
сигналом после минимального размера либо на первом допустимом рубеже около целевого размера
(стартовый ориентир 2–4K tokens). Не использовать LLM для перекройки старых границ.
Открытая последняя page не кэшируется как sealed. Пересечение page с recent tail остаётся raw;
по мере роста истории sealed prefix не переупаковывается. Огромный atomic turn обрабатывается
явной oversized policy, а не делением tool chain.

`segment_hash` вычисляется по полному canonical source, включая роли, IDs, tool args и blocks,
до lossy compression. Key representation включает namespace доверия, hash source, версии
canonicalization/segmentation/tool compression, prompt/schema, summarizer identity/version
и generation parameters. Source hash и representation key — разные понятия.

На segment хранятся отдельно:

- factual provider-facing summary: решения, причины, незакрытые задачи, ошибки и exact anchors;
- retrieval synopsis: темы/термины для поиска, не автоматически provider-visible;
- metadata: source range, tokens, versions, provenance, completeness; embedding позже.

LLM не гарантирует byte stability даже при temperature=0. Её обеспечивает сохранённый
неизменяемый результат: singleflight на key, atomic first-success write, hit возвращает
точные bytes. Не пересуммаризировать sealed pages при append; mutation меняет соответствующие
keys, неизменившиеся content-addressed pages переиспользуются. После eviction допустима
новая генерация; byte stability гарантируется на срок жизни cache entry, включая restart при
включённом persistence. Не обещать одновременно конечный cache и вечную сохранность bytes.

Cache ограничен по размеру/TTL, с quota и изоляцией tenant/trust scope. Сейчас один client key
означает один доверенный deployment scope; `prompt_cache_key` не является identity.
Persistent cache opt-in: summaries содержат пользовательские данные, каталог с правами 0700,
файлы 0600, atomic writes, corruption recovery и очистка. Raw history по умолчанию только
в памяти запроса, никогда в journal. Данные cache не попадают в Nix store.

## Working set

`context-select` строит query из последнего user request, используя bounded recent context
для разрешения ссылок вроде «тот файл» (в том числе на запросах, заканчивающихся tool result).
BM25 индексирует synopsis и exact anchors только segments текущей source history; cache не
является общим поиском по другим сессиям. Tie-break deterministic, выдача собирается
хронологически, recent pages не дублируются.

Итог: неизменённые инструкции + bounded global compact history + выбранные relevant summaries
+ exact recent raw tail. Global compact в MVP — детерминированная ограниченная по budget
выборка структурированных facts (актуальные решения, ограничения, unresolved items) из cached
representations с provenance и порядком событий; это не конкатенация всех summaries и не новый
giant summarizer на каждый запрос. Старое и новое противоречащие утверждения не сливаются
в выдуманный факт. Detailed selected representation заменяет уже включённую копию, не дублирует её.

Recent/instructions/current query резервируются первыми; затем bounded global facts, затем
ranked relevant segments, пока хватает budget. No-hit не означает отправку всех summaries.
Generated history оформляется явно помеченным блоком исторических данных без system/developer
роли и без orphan tool messages. Формат и совместимость с Bifrost/provider adapters фиксируются
тестами. Prompt injection внутри tool output остаётся недоверенными данными и в summarizer,
и в provider-facing representation.

После MVP: `absent -> compact -> detailed -> raw page`, tiered eviction/cold pages, embeddings,
hybrid retrieval и semantic prefetch. Raw promotion берёт страницу из source данного запроса,
сохраняет tool chain и вытесняет менее полезные detail pages, не protected tail.
Hierarchical cached summaries по hashes детей позволяют масштабировать global overview.

## Routing, внутренние вызовы и эксплуатация

Один подготовленный body используется всеми обычными attempts; race/hedge не запускают свои
summarizers. Внутренний summarizer вызывает существующий Runner/Executor без HTTP loopback и
с явным bypass transformation; отдельные allowlisted targets, timeout, concurrency, token/cost
и attempts budgets. Нельзя неявно расширять право на отправку данных другому provider.
Ошибки внутренних calls не записываются как ошибки выбранного основного provider.
Внутренний usage учитывается отдельно и не подмешивается в usage ответа клиенту.

Существующий [continue](./../../packages/llm-gateway/router_stream.go) добавляет partial assistant
output к отправляемому body. Он использует подготовленный working set, не запускает semantic
pipeline повторно и не теряет уже выданный partial. Перед takeover нужен повторный accounting
с резервом для accumulated partial/output. При переполнении — существующий терминальный SSE
error contract, без повторной отправки старого oversized source или ложного `[DONE]`.
Cancellation клиента отменяет preparation и его внутренние вызовы; timeout preparation отдельно
ограничен, но входит в общий request deadline и latency/TTFT.

Nix-конфигурация opt-in на logical model, включая `smart` на homelab; существующие flat
routing actions не превращаются в transformer protocol. Нужны typed options и module assertions,
Go validation для прямого JSON, runtime config generation и безопасный cache lifecycle.
Метрики продолжают [F12](./../f12-observability/README.md): before/after tokens, stage latency,
cache hit/miss, summary calls/usage, bypass/error/overflow. Только bounded labels; ни hashes,
ни prompt/summary, ни user IDs в labels/logs; request_id только в событиях.

## Задачи и порядок

| Задача | Результат | Зависимости |
| --- | --- | --- |
| [f16-01](./f16-01-context-contract-budget.md) | Контракты, accounting, capability/budget policy | существующая F7 |
| [f16-02](./f16-02-context-turns-segments.md) | Canonical turns, raw tail, stable pages | f16-01 |
| [f16-03](./f16-03-tool-output-compress.md) | Deterministic old tool compression | f16-02 |
| [f16-04](./f16-04-history-compact-cache.md) | Segment summary/synopsis и cache | f16-02, f16-03 |
| [f16-05](./f16-05-context-select.md) | BM25 и budgeted assembly | f16-01, f16-04 |
| [f16-06](./f16-06-context-routing-integration.md) | Pre-routing hook, failures, continue | f16-03, f16-04, f16-05 |
| [f16-07](./f16-07-context-nix-config.md) | Typed Nix config и rollout controls | f16-01; завершение после f16-06 |
| [f16-08](./f16-08-context-observability.md) | Metrics/events и сравнение стоимости | f16-06, F12 |
| [f16-09](./f16-09-context-acceptance.md) | Контрактные проверки и opt-in acceptance | f16-06, f16-07, f16-08 |

MVP завершён после f16-09. Tasks включают проверки своего контракта по
[политике тестов](./../../tests/README.md); f16-09 проверяет интеграцию и качество, не откладывает
все тесты до конца. Embeddings, hierarchical summaries, diff-based outputs и automatic raw
promotion не блокируют MVP. Универсальная plugin-система, ext_proc, transformer protocol,
внешний `/compress`, отдельные proxies и explicit JSON state вместо истории не входят в план.

## References

Источники идей проверены 2026-09-21; это направления адаптации, а не новые runtime dependencies:

- [virtual-context](https://github.com/virtual-context/virtual-context): canonical turns,
  recent window, segment/factual summaries и retrieval; session/proxy lifecycle не переносим.
- [llm-mmu](https://github.com/Kaseban/llm-mmu): resident/cold pages, tiered eviction,
  cached representations и promotion; его proxy не разворачиваем.
- [context-compress](https://github.com/Open330/context-compress): format-aware обработка
  tool output; конкретные эвристики проверяем на наших Go fixtures, не смешиваем с semantic memory.

TokenMizer decision graph оставляем возможным будущим semantic-memory слоем.
skillstate-proxy не используем как основу: explicit JSON state не заменяет полную историю.
Перед переносом кода из references проверить license и зафиксировать revision; сейчас
заимствуются архитектурные идеи, зависимости не добавляются.
