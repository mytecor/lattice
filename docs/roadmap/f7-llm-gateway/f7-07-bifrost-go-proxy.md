# Реализовать собственный Go proxy поверх Bifrost и выполнить cutover

Фича: [F7 — LLM gateway](./README.md). Продолжает отрицательный Go LIP PoC
[f7-06](./f7-06-go-lip-gonka-cutover.md), завершает runtime-часть
[f7-02](./f7-02-declarative-gateway-service.md) и разблокирует
[f7-04](./f7-04-routing-resilience-tests.md) и [f8-02](../f8-pi-runtime/f8-02-pi-gateway-config.md).

## Решение

Не развивать текущие patches для `mxyhi/token_proxy` и не переходить на Go LIP, AgentCC,
Aiproxer или другой готовый gateway. Реализовать небольшой Lattice-owned Go HTTP proxy и
использовать Bifrost через его Go API как библиотечный provider execution layer.

Bifrost отвечает за поддерживаемые им provider adapters, schema conversion, streaming transport,
credentials hooks, plugins и telemetry. Lattice proxy отвечает за OpenAI-compatible HTTP ingress,
logical/native model projection, model discovery policy и композицию маршрутов. Parallel race
строится над отдельными Bifrost calls с общим отменяемым context. Не форкать Bifrost, не ставить
ещё один proxy перед его готовым HTTP gateway и не реализовывать race dynamic plugin с рекурсивным
HTTP-вызовом.

Перед закреплением зависимости проверить актуальный публичный Bifrost Go API по официальной
документации и исходникам. Имена Go types/methods из исследовательских заметок не являются
контрактом; выбранная версия фиксируется воспроизводимо вместе с license/source metadata.

## Клиентский контракт

- Единственная LAN-точка входа остаётся `http://llm-gateway.<node-name>.local/v1`; Caddy владеет
  внешним портом 80, proxy слушает только loopback.
- Обязательны `GET /v1/models`, Chat Completions и Responses, включая SSE streaming. Остальные
  endpoints добавляются только по подтверждённой потребности Pi.
- Pi и workers видят ровно `stupid`, `standard` и один gateway client
  credential. Native provider/model IDs, внутренние URLs и route selectors не публикуются.
- Входящий logical model преобразуется в native target до Bifrost call. Во всех успешных ответах,
  streaming events, безопасных ошибках и diagnostics восстанавливается исходное logical имя.
- Неизвестное или provider-specific имя отклоняется до обращения к upstream.

## Декларативная модель

Открытая конфигурация разделяет providers, logical model mappings и плоский routing pipeline.
Provider имеет независимые `inference_url` и опциональный `models_url`, поэтому OpenBroker может
делать inference через `api.openbroker.gonka.gg`, а список той же access group загружается с
`proxy.gonka.gg/v1/models`.

Минимальная форма, которую нужно типизировать и валидировать:

```yaml
providers:
  - id: gonka-openbroker
    name: gonka
    inference_url: https://api.openbroker.gonka.gg
    models_url: https://proxy.gonka.gg/v1/models
    api_key: env.OPENBROKER_GONKA_GG_API_KEY
    models_api_key: env.PROXY_GONKA_GG_API_KEY
  - id: gonka-proxy
    name: gonka
    inference_url: https://proxy.gonka.gg
    api_key: env.PROXY_GONKA_GG_API_KEY

models:
  - match: {provider: gonka, id: MiniMaxAI/MiniMax-M2.7}
    override: {id: stupid}
  - match: {provider: gonka, id: deepseek-ai/DeepSeek-V4-Flash-0731}
    override: {id: standard}

routing_rules:
  - match: {model: standard}
    action: race
    providers: [gonka-openbroker, gonka-proxy]
  - match: {model: standard}
    action: retry
    attempts: 10
    on: [timeout, connection_error, 429, 5xx]
    backoff: {type: exponential, initial: 100ms, max: 1s}
  - match: {model: standard}
    action: fallback
    providers: [backup]
    on: [timeout, connection_error, 429, 5xx]
```

`routing_rules` исполняется по порядку как преобразование текущего route. Один rule выполняет
одно action: минимум `race`, `retry`, `fallback`; схема должна допускать `timeout` и `hedge` без
перехода к рекурсивным nested fallbacks. Сохраняются обязательные требования F7: несколько
upstream, priority groups, cooldown/circuit breaker, unconditional race, hedging, retry/backoff,
fallback и streaming.

## Race и streaming semantics

- Non-streaming `race` запускает все targets одновременно и возвращает первый успешный валидный
  ответ, а не первую завершившуюся ошибку.
- После выбора победителя все проигравшие calls отменяются через context cancellation; send в
  result channel не должен блокироваться после возврата handler.
- Streaming winner выбирается по первому meaningful content, reasoning или tool-call event, а не
  по пустому/служебному chunk и не по завершению generation.
- До выбора победителя клиенту ничего не отправляется. После выбора stream победителя передаётся
  без перестановки событий, проигравшие streams отменяются и дренируются/закрываются безопасно.
- Client disconnect отменяет winner и все ещё работающие branches.
- Branch errors агрегируются без provider credentials, внутренних URLs и native IDs. Retry и
  fallback как минимум различают `timeout`, `connection_error`, `429` и `5xx`.

## Model discovery и same-group fallback

- Публичный `/v1/models` строится самим Lattice proxy и содержит только `stupid`, `standard`.
- Upstream catalogs остаются внутренними. Использовать Bifrost model discovery там, где его
  публичный Go API подходит, и тонкий discovery adapter для независимого `models_url` там, где
  inference endpoint не предоставляет `/v1/models`.
- Каждая logical model имеет explicit primary native model и access group.
- При исчезновении primary разрешены только доступные модели той же access group. Переход в
  другую группу без явного rule запрещён; пустая группа приводит к fail closed.
- Кандидаты нормализуются, дедуплицируются и сортируются лексикографически по native model ID.
  Выбранный fallback детерминирован и стабилен до следующего успешного refresh.
- Refresh выполняется каждые 10 минут; есть ручной немедленный refresh. Ошибка сохраняет
  last-known-good snapshot. Возвращение primary переключает logical model обратно на primary.

## Credentials и диагностика

- Несекретные providers, mappings и routing rules остаются typed Nix/config data в Git.
- Gateway client key и каждый provider key/OAuth credential поступают отдельными
  agenix/systemd credentials и не попадают в Nix store, generated world-readable config, argv,
  logs или errors.
- Поддерживаемые Bifrost plugins и telemetry подключаются внутри execution layer, но не получают
  права менять публичный model contract.
- Health/metrics/logs показывают logical route, action, безопасный error class и cancellation,
  но не prompt/content, credentials, internal endpoints или native IDs.

## Что сделать

### 1. Core и HTTP facade

- [x] Закрепить совместимую Bifrost version/source и собрать минимальный Go binary.
- [x] Реализовать typed config loading/validation и компиляцию flat rules в execution plan.
- [x] Реализовать logical model registry, internal catalog snapshots и безопасный refresh.
- [x] Реализовать Chat Completions, Responses, `/v1/models`, non-streaming и streaming paths.
- [x] Реализовать race, retry, fallback, timeout, priority, cooldown/circuit breaker и hedge.

### 2. Воспроизводимая проверка

- [x] Mock upstreams доказывают фактический одновременный старт race, first-success semantics,
  отсутствие раннего отказа по первой ошибке, отмену проигравших и отсутствие goroutine leaks.
- [x] Streaming tests покрывают первый meaningful content/reasoning/tool-call event, порядок SSE,
  ошибку до winner, обрыв начатого stream и client disconnect.
- [x] Pipeline tests покрывают retry/backoff, fallback, priority, cooldown/circuit breaker,
  timeout, hedge/TTFT и error classification.
- [x] Contract tests покрывают Chat, Responses, оба streaming path, request/response model rewrite,
  неизвестные IDs и отсутствие внутренних данных.
- [x] Discovery tests покрывают разные `inference_url`/`models_url`, refresh 10 минут, manual
  refresh, last-known-good, стабильный same-group fallback, возврат primary и fail closed.
- [ ] Security tests подтверждают отсутствие provider keys и prompts в logs, diagnostics, errors,
  process args, generated config и Nix store references.

### 3. NixOS и прямой cutover

- [x] Перевести существующий `llm-gateway` package/module/profile и VM-test на новый binary,
  сохранив unit name, loopback port, Caddy route и credential boundary.
- [ ] Описать bootstrap, rotation, model refresh, health, cutover и rollback.
- [ ] До activation проверить closure, decryptability secrets, fake-upstream matrix и доступную
  rollback generation.
- [ ] Одной NixOS activation остановить `token_proxy` и запустить новый binary; два production
  runtime одновременно не держать.
- [ ] С Mac проверить mDNS, `/v1/models`, Chat/Responses streaming через Pi, scoped Gonka race,
  retries/fallbacks, закрытый backend port и безопасную диагностику.
- [x] После успешной runtime-проверки удалить `token-proxy-src`, package, patches, Go LIP notes из
  активной конфигурации и другие runtime-specific artifacts. Исторические task findings сохранить.

  Удалено 2026-09-07: input `token-proxy-src` и пакет `pkgs.lattice.token-proxy` из корневого
  `flake.nix`/`flake.lock`, `packages/token-proxy/` (package.nix и оба patches), упавший spike-test
  `tests/token-proxy-spike.{nix,py}` и его запись в `checks.x86_64-linux`, а также раздел о
  `token-proxy` в `packages/README.md`. Полная вычистка legacy runtime из модуля, профиля и
  оставшейся документации — в [f7-08](./f7-08-remove-token-proxy.md). Исторические task findings
  (`f7-01-token-proxy-spike.md`, `f7-05`, `f7-06`) сохранены неизменными.

## Критерий готовности

- Одна flake check команда воспроизводимо покрывает обязательную routing/streaming/discovery/
  security matrix без внешних provider credentials.
- Homelab обслуживает стабильный logical-model API через собственный Go proxy и Bifrost Go API;
  Pi не знает provider topology или credentials.
- Race действительно параллельный, streaming winner выбирается по meaningful event, а loser и
  client-disconnect cancellation наблюдаемы тестом.
- Dynamic discovery не зависит от `/v1/models` на inference endpoint; десятиминутный refresh,
  last-known-good и детерминированный same-group fallback подтверждены.
- `token_proxy` и Go LIP отсутствуют в active system closure/config; rollback возможен через
  предыдущую NixOS generation.
- Документация, roadmap status и эксплуатационные инструкции соответствуют запущенному runtime.

## Открытые вопросы

_нет архитектурных_. Точные Bifrost package/API names и численные timeout/hedge/circuit-breaker
значения выбираются реализацией и фиксируются тестами; они не меняют принятое разделение
ответственности.

## Статус

Реализация начата 2026-09-07. Добавлен `packages/llm-gateway` на Go 1.27 с закреплённым Bifrost
Core v1.8.4, typed JSON config, independent inference/models URLs, internal last-known-good catalog,
flat routing pipeline, Chat/Responses HTTP facade и streaming winner selection. Unit/integration
tests подтверждают Bifrost custom provider, parallel first-success race, loser cancellation,
meaningful streaming selection, retry, fallback, hedge и logical model projection.

Nix overlay экспортирует `pkgs.lattice.llm-gateway`; модуль получил opt-in runtime `bifrost`,
отдельные credentials для inference/discovery и новый evaluation/VM test. Homelab пока остаётся на
legacy runtime до изменения node config; native mappings двух logical models теперь утверждены и
внесены. Для завершения cutover остаётся Linux build/VM run и применение на homelab. Полный Darwin `buildGo127Module`, `nix flake check --no-build`, derivation
evaluation, обычные Go tests и `go test -race` проходят. Linux package/VM не запускались: текущий
host — `aarch64-darwin`, Linux remote builder не настроен.

Live smoke 2026-09-07 запустил собранный Nix binary на loopback и успешно обновил внутренний
catalog с `https://proxy.gonka.gg/v1/models` без inference credential. Активный production mapping
после пользовательского решения: `stupid → MiniMaxAI/MiniMax-M2.7`,
`standard → deepseek-ai/DeepSeek-V4-Flash-0731`; `/healthz` не раскрывает topology, а
`/v1/models` публикует только эти два logical ID.
