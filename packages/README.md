# Lattice Packages

Каталог `packages/` хранит пакеты, которых нет в общем репозитории Nix, локальные изменения, патчи и сборки внешних зависимостей.

Каждый пакет живёт в отдельной директории как функция для `callPackage`. Корневой flake подключает
каталог с `flake = false`, добавляет пакеты в overlay `pkgs.lattice` и экспортирует их через
`packages.${system}`. Пример — [`rns-rs/package.nix`](./rns-rs/package.nix).

## Закреплённая версия Reticulum

`rns-server` и `rnsh` собираются из одного snapshot `rns-rs`, зафиксированного по commit и хешам
исходников/Cargo-зависимостей в `package.nix`. Обновление от 2026-10-04 использует release commit
`3ddb784b19203d72b2ef119dbd3c3dc6a39a21dd`: `rns-server` 0.3.3 и `rns-cli` 0.4.3 (бинарник `rnsh`).
В вывод `--version` передаётся версия соответствующего Cargo-пакета и короткий commit hash:
GitHub-архив не содержит `.git`, из которого upstream обычно вычисляет номер сборки.

Пакет сервера содержит два локальных исправления:

- `shared-local-delivery.patch` доставляет HEADER_1 LINKREQUEST/DATA к прикреплённому локальному
  клиенту. Последний публичный transport-узел может снять транспортный заголовок перед доставкой;
  без этого исправления shared daemon не передавал запрос rnsh-слушателю. Регрессионный тест
  воспроизводит сбой на исходном snapshot. Эти тесты также включены в Nix-сборку сервера.
- `darwin-local-client.patch` (только macOS) переключает принятый TCP-сокет shared instance
  в blocking mode. Иначе унаследованный `O_NONBLOCK` даёт `EAGAIN`, и локальный клиент отключается.

Исправления rnsh из [PR #142](https://github.com/lelloman/rns-rs/pull/142) теперь входят в
закреплённый upstream snapshot. Listener изолирует ошибку одной сессии, повторяет отправку
при `NotReady` и сохраняет накопленный вывод без искусственного порога. Локальные
`rnsh-session-send.patch` и `rnsh-backpressure.patch` удалены. В upstream также включены
исправления типов аргументов libc, поэтому прежние Nix-подстановки для macOS удалены.

Все 34 теста rnsh входят в Nix-сборку. До слияния на homelab были проверены две одновременные
сессии: 4000 строк (18 893 байта) доставлены без потерь и дубликатов, получен удалённый
exit status 17, listener не перезапускался. История проверки — в
[#140](https://github.com/lelloman/rns-rs/issues/140) и
[#141](https://github.com/lelloman/rns-rs/issues/141).

На Mac оператор может использовать установленный Python RNS 1.5.2 (`rnsd`/`rnsh`) через тот же
публичный реестр. Rust-бинарники для Darwin собирались в Nix store для тестов и не подменяли PATH.

## LLM gateway

Собственный Lattice-owned Go proxy [`llm-gateway`](./llm-gateway/README.md) собирается из
`packages/llm-gateway` (pre-fixed `buildGo127Module`) и обслуживает OpenAI-compatible логические
модели поверх [Bifrost Core](https://github.com/maximhq/bifrost) Go API. Пакет экспортируется как
`pkgs.lattice.llm-gateway` из корневого `flake.nix`. Подробности API, routing, discovery и
проверка — в его собственном README.

Прежний runtime-кандидат `mxyhi/token_proxy` (input `token-proxy-src`, headless CLI, два патча и
executable spike-test) удалён из активной конфигурации после cutover на Go proxy; исторические
находки сохранены в задачах `f7-01`/`f7-05`/`f7-06`. Подробности cutover — в
[`roadmap/f7-llm-gateway/f7-07-bifrost-go-proxy.md`](../roadmap/f7-llm-gateway/f7-07-bifrost-go-proxy.md).

## Node status

[`node-status`](./node-status/README.md) — Lattice-owned Go API и Prometheus exporter системных
показателей. Пакет экспортируется как `pkgs.lattice.node-status` и `packages.<system>.node-status`;
NixOS-интеграция и публичный status ingress находятся в
[`profiles/app-services`](../profiles/app-services/README.md).

## agentrun-openai

[`agentrun-openai`](./agentrun-openai/README.md) — OpenAI-compatible HTTP-шлюз над
[`github.com/dmora/agentrun`](https://github.com/dmora/agentrun) (форк
[`github.com/mytecor/agentrun`](https://github.com/mytecor/agentrun)); предоставляет
persistent Claude Code / Codex / Antigravity сессии как модели OpenAI API. Пакет пинится на
release `v0.1.1` внешнего репозитория (см. `package.nix`). NixOS-интеграция —
[`modules/agentrun-openai`](../modules/agentrun-openai/README.md), профиль —
[`profiles/agentrun-openai`](../profiles/agentrun-openai/README.md).

## Muse Code

[Muse Code](./muse/README.md) — официальный закреплённый Linux CLI Meta.
[Muse ACP](./muse-acp/README.md) подключает его к agentrun-openai через
MSP/ACP с отключёнными встроенными инструментами и отдельным persistent state.

## Antigravity CLI

[`antigravity`](./antigravity/README.md) — официальный бинарный дистрибутив Google Antigravity CLI (`1.3.5`).
Устанавливает в `bin` исполняемый файл `antigravity` и симлинк `agy`.

## Antigravity ACP

[`antigravity-acp`](./antigravity-acp/README.md) — официальный Agent Client Protocol (ACP) сервер Google Antigravity (`1.4.0`),
включающий бинарники `agy_acp_server.par` и `localharness_external`.
Для работы со шлюзом [`agentrun-openai`](./agentrun-openai/README.md) пакет предоставляет враппер
`antigravity-acp-fsless`, который изолирует рабочее окружение `$GEMINI_HOME` и отключает встроенные
инструменты харнесса через потоковый JSON-RPC фильтр, гарантируя подключение только клиентских MCP-инструментов.

## Gemini CLI

[`gemini-cli`](./gemini-cli/README.md) — официальный CLI-дистрибутив Google Gemini CLI (`@google/gemini-cli` `0.64.0`),
собранный через общий [`pnpm-cli-builder`](./pnpm-cli-builder/README.md). Устанавливает в `bin` исполняемый файл `gemini`.

## Gemini ACP

[`gemini-acp`](./gemini-acp/README.md) — запуск Gemini CLI в режиме Agent Client Protocol (ACP) по флагу `--acp`.
Для интеграции со шлюзом [`agentrun-openai`](./agentrun-openai/README.md) пакет предоставляет враппер
`gemini-acp-fsless`, отключающий локальную файловую систему, выполнение bash и все встроенные инструменты
через Admin Policy Engine (`fsless.toml`), конфигурацию `tools.exclude` в `$GEMINI_CLI_HOME/.gemini/settings.json`
и протокольный фильтр [`filter.mjs`](./gemini-acp/filter.mjs).

## Agent runtime OCI image

[`packages/agent-image`](./agent-image/README.md) собирает воспроизводимый OCI-образ агентского
рантайма через `pkgs.dockerTools.buildLayeredImage` для выполнения в `r1s`
([`roadmap/f10-disposable-worker/f10-04-agent-runtime-acp.md`](../roadmap/f10-disposable-worker/f10-04-agent-runtime-acp.md)).
Образ распространяется через локальный IPFS-фасад реестра (`lattice.ipfs-registry-facade`)
и публикуется с помощью `publish-agent-image`.

## git-watchd

[`git-watchd`](./git-watchd/README.md) — generic-адаптер между локальными Git-репозиториями и шиной
сообщений Meshbus ([`roadmap/f11-git-task-pipeline/f11-02-git-watchd.md`](../roadmap/f11-git-task-pipeline/f11-02-git-watchd.md)).
Отслеживает изменения Git-ревизий и предоставляет сервисам `git.head` и `git.watch`.

## task-contract

[`task-contract`](./task-contract/README.md) — пакет неизменяемых типов (`TaskSpec`, `TaskResult`),
парсера и валидатора контрактов задач `task.md` в Git для pipeline задач
([`roadmap/f11-git-task-pipeline/f11-01-task-contract.md`](../roadmap/f11-git-task-pipeline/f11-01-task-contract.md)).
Пакет также включает CLI-валидатор `task-validate`.

## taskd
 
[`taskd`](./taskd/README.md) — Git-reconciler между состоянием репозитория задач и слоем
исполнения `agentd`
([`roadmap/f11-git-task-pipeline/f11-03-taskd.md`](../roadmap/f11-git-task-pipeline/f11-03-taskd.md)).
Переводит неизменяемые спецификации задач `task.md` в `TaskSpec` и отправляет в `agentd` через Meshbus.

## agentd

[`agentd`](./agentd/README.md) — демон исполнения задач в Lattice
([`roadmap/f11-git-task-pipeline/f11-04-dummy-pipeline.md`](../roadmap/f11-git-task-pipeline/f11-04-dummy-pipeline.md)).
Слушает запросы `TaskSpec` по Meshbus, исполняет рабочую нагрузку (детерминированный dummy workload / r1s) и возвращает `TaskResult`.


## pnpm CLI

[`packages/pnpm-cli-builder`](./pnpm-cli-builder/README.md) предоставляет общий `buildPnpmCli`
для воспроизводимой сборки CLI из npm registry. Каждый CLI хранит
package-specific lock и описание сборки в собственном каталоге; первая реализация —
[`packages/pi`](./pi/README.md). Зависимости fetch’ятся как fixed-output derivation, затем pnpm
работает offline в Nix sandbox; Node.js и pnpm приходят из закреплённого `nixpkgs`.

Остальные системные пакеты обновляются через input `nixpkgs` в корневом `flake.lock`.
Изменение закреплённых версий само по себе не переключает работающую ноду: новую конфигурацию
применяет обычный процесс развёртывания через `comin` после публикации в `main`.
Для release snapshot `3ddb784b1920` этот путь повторно проверяется F10 live smoke: обновление
содержит upstream shared-client fixes для path request, one-hop routing и transport headers.

## Moli

[Moli](./moli/README.md) — headless-браузер с CDP, реальным layout и скриншотами;
внешние discovery-адреса адаптирует общий [browser-ingress](./browser-ingress/README.md),
который также обслуживает Foxbridge и страницу выбора браузера.
