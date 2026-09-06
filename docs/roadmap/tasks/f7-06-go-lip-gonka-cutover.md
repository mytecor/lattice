# Проверить Go LIP на Gonka и заменить `token_proxy`

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md). Зависит от source audit
[f7-05](./f7-05-research-gateway-alternatives.md). Блокирует завершение
[f7-04](./f7-04-routing-resilience-tests.md) и подключение Pi в
[f8-02](./f8-02-pi-gateway-config.md).

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

## Открытые вопросы

- Может ли Go LIP публиковать только четыре logical IDs и выполнять request/response rewrite без
  instance prefix и без fork core.
- Проходит ли unknown logical model и отсутствующий native target fail-closed через alias/parallel
  selector.
- Есть ли приемлемый immutable release; при отсутствии release допустим ли production pin commit.

**Статус:** открыта 2026-09-06; готова к передаче другому агенту.
