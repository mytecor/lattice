# Исследовать альтернативы `token_proxy`

Фича: [F7 — LLM gateway](./README.md). Продолжает
[f7-01](./f7-01-token-proxy-spike.md) после проверки `token_proxy` в реальной конфигурации.
Executable-проверка готового кандидата вынесена в
[f7-06](./f7-06-go-lip-gonka-cutover.md); последующее решение о собственном proxy поверх Bifrost
Go API закреплено в [f7-07](./f7-07-bifrost-go-proxy.md).

## Контекст

Закреплённый `mxyhi/token_proxy` прошёл первоначальный executable spike, но при интеграции двух
Gonka endpoint обнаружилась недостаточная модель конфигурации. `api.proxy.gonka.gg` предоставляет
динамический `/v1/models`, а OpenBroker обслуживает те же модели без собственного model catalog.
Текущий runtime не умеет безопасно связать один динамический каталог с несколькими upstream и
ограничить `race` только этой группой. Пустой `available_models` означает wildcard, поэтому после
добавления providers с другими моделями Gonka может ошибочно участвовать в их маршрутизации.

Отдельный catalog URL также нельзя задать независимо от request endpoint и credential. Простая
подмена URL отправила бы credential OpenBroker на другой host. Кроме того, для headless runtime
уже поддерживаются локальные patches, что увеличивает стоимость дальнейшего расширения форка.

## Что проверить

- [x] Найти минимум три активно поддерживаемых self-hosted gateway с OpenAI-compatible ingress.
- [x] Сверить динамический model discovery, routing, race/hedging, retries, credential isolation,
  deployment model и лицензию по документации и текущим исходникам.
- [x] Отделить реализованные возможности от заявлений README.
- [x] Выбрать кандидата для отдельного executable PoC на сценарии Gonka Proxy/OpenBroker.

## Результат source audit

Проверка выполнена 2026-09-06 по актуальным upstream repositories. Ни один кандидат не считается
выбранным без PoC, но shortlist удалось сузить:

| Кандидат | Наблюдаемые свойства | Решение |
| --- | --- | --- |
| [Go LLM Interactive Proxy](https://github.com/matdev83/go-llm-interactive-proxy) | Apache-2.0 single binary; OpenAI Chat/Responses; remote model inventory с background refresh и last-known-good; regex aliases; streaming parallel selector `!`; retries, circuit breaker, diagnostics | Первый кандидат для PoC |
| [LLM Interactive Proxy / Aiproxer](https://github.com/aiproxer/aiproxer) | Streaming race по first meaningful output с отменой проигравших; retries/failover/circuit breaker; динамические каталоги отдельных connectors | Резервный кандидат; Python runtime и AGPL-3.0 |
| [AgentCC Gateway](https://github.com/future-agi/future-agi/tree/main/agentcc-gateway) | Apache-2.0 Go binary; per-provider startup discovery; retry/failover; исходник содержит `RaceExecutor` | Не брать в PoC сейчас: каталог не наследуется между providers, а `RaceExecutor.Execute` не подключён к request path в проверенном tree |
| [Bifrost](https://github.com/maximhq/bifrost) | Apache-2.0 Go gateway и Go API; provider adapters, schema conversion, streaming и model discovery | Готовый HTTP gateway не реализует нужный race; Go API выбран библиотечным execution layer для собственного proxy в f7-07 |
| [LiteLLM](https://github.com/BerriAI/litellm) | Зрелый OpenAI-compatible proxy; model groups, retries, fallback, cooldown, много providers | Не подходит без отдельного race layer: router выбирает один deployment, группы задаются декларативно |
| [Portkey Gateway](https://github.com/Portkey-AI/gateway) | MIT gateway; retries, fallback, weighted load balancing и conditional routing | Не подходит: нет требуемого streaming race и динамического наследования каталога |

Для Go LIP найден потенциальный путь к scoped race, но не готовый клиентский mapping. Backend
`gonka-proxy` публикует динамический inventory с Proxy. Стандартный `/v1/models` Go LIP добавляет
backend instance к canonical ID и возвращает имя вида `gonka-proxy:gonka/<native-model>`. Regexp
alias может преобразовать конкретное логическое имя в scoped parallel selector с выбранным native
model:

```yaml
model_aliases:
  - pattern: '^cheap$'
    replacement: 'gonka-proxy:gonka/<native-model>!gonka-openbroker:<native-model>'
```

Первая B-leg использует registry mapping canonical ID в native ID; второй B-leg alias передаёт тот
же native ID OpenBroker напрямую. Такой selector позволяет ограничить race выбранным logical
route, а не всеми моделями протокола.

Однако `model_aliases` обрабатывает входящий selector, но сам по себе не добавляет alias в
`/v1/models`. Публичный контракт Lattice остаётся ровно `cheap`, `standard`, `strong`, `frontier`;
динамические catalog IDs, native model IDs и backend instances должны оставаться внутренними.
Исходники не подтвердили поддерживаемую конфигурацию такой проекции без fork core. Это отдельный
блокер F7-06 наряду с проверкой request/response rewrite и участием backend без собственного
inventory.

Проверенный commit Go LIP: `d784a8344888dd9de2141a13d4bf723125d4b08c` от 2026-09-04.
На момент audit в repository не найден release tag, поэтому PoC закрепляет commit напрямую и не
считает отсутствие опубликованного release приемлемым для финального production choice без
отдельного решения.

## Критерий готовности

- [x] Есть сравнительная матрица минимум пяти кандидатов с прямыми ссылками на upstream.
- [x] Выявлены обязательные пробелы AgentCC, Bifrost, LiteLLM и Portkey для сценария Lattice.
- [x] Go LIP выбран первым кандидатом, Python LIP — резервным.
- [x] Непроверенные свойства явно перенесены в executable-задачу, а не объявлены рабочими.

## Затрагиваемые файлы / слои

- `ARCHITECTURE.md`
- `docs/roadmap/BACKLOG.md`
- `docs/roadmap/f7-llm-gateway/README.md`
- `docs/roadmap/tasks/`

## Последующее решение

Go LIP отклонён в [f7-06](./f7-06-go-lip-gonka-cutover.md): обязательные `/v1/models` projection и
десятиминутный catalog refresh требуют функционального fork. Резервные готовые gateway больше не
исследуются. Lattice реализует собственный Go HTTP proxy, а Bifrost использует через Go API как
provider execution library; план и cutover находятся в [f7-07](./f7-07-bifrost-go-proxy.md).

**Статус:** завершена 2026-09-06; исторический source audit, решение обновлено в f7-07.
