# Lattice Packages

Каталог `packages/` хранит пакеты, которых нет в общем репозитории Nix, локальные изменения, патчи и сборки внешних зависимостей.

Каждый пакет живёт в отдельной директории как функция для `callPackage`. Корневой flake подключает
каталог с `flake = false`, добавляет пакеты в overlay `pkgs.lattice` и экспортирует их через
`packages.${system}`. Пример — [`rns-rs/package.nix`](./rns-rs/package.nix).

## Закреплённая версия Reticulum

`rns-server` и `rnsh` собираются из одного snapshot `rns-rs`, зафиксированного по commit и хешам
исходников/Cargo-зависимостей в `package.nix`. Обновление от 2026-09-05 использует commit
`042e37047b70ea0e06b9aff0aed6214bc305ab35`: `rns-server` 0.3.1 и `rns-cli` 0.4.1 (бинарник `rnsh`).
Это snapshot основной ветки; суффикс `unstable` в версии пакета сохраняется.
В вывод `--version` передаётся версия соответствующего Cargo-пакета и короткий commit hash:
GitHub-архив не содержит `.git`, из которого upstream обычно вычисляет номер сборки.

Пакет сервера содержит два локальных исправления:

- `shared-local-delivery.patch` доставляет HEADER_1 LINKREQUEST/DATA к прикреплённому локальному
  клиенту. Последний публичный transport-узел может снять транспортный заголовок перед доставкой;
  без этого исправления shared daemon не передавал запрос rnsh-слушателю. Регрессионный тест
  воспроизводит сбой на исходном snapshot; после патча проходят все 656 unit-тестов `rns-core`.
  Эти тесты также включены в Nix-сборку сервера.
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
[`docs/roadmap/tasks/f7-07-bifrost-go-proxy.md`](../docs/roadmap/tasks/f7-07-bifrost-go-proxy.md).

## pnpm CLI

[`packages/pnpm-cli-builder`](./pnpm-cli-builder/README.md) предоставляет общий `buildPnpmCli`
для воспроизводимой сборки CLI из npm registry. Каждый CLI хранит
package-specific lock и описание сборки в собственном каталоге; первая реализация —
[`packages/pi`](./pi/README.md). Зависимости fetch’ятся как fixed-output derivation, затем pnpm
работает offline в Nix sandbox; Node.js и pnpm приходят из закреплённого `nixpkgs`.

Остальные системные пакеты обновляются через input `nixpkgs` в корневом `flake.lock`.
Изменение закреплённых версий само по себе не переключает работающую ноду: новую конфигурацию
применяет обычный процесс развёртывания через `comin` после публикации в `main`.
Для snapshot `042e37047b70` этот путь проверен 2026-09-05: homelab автоматически применил
`ba03779`, и оба работающих бинарника сообщили ожидаемую ревизию.
