# Hydra ACP package

[`@hydra-acp/cli`](https://www.npmjs.com/package/@hydra-acp/cli) собирается через общий
[pnpm CLI builder](../pnpm-cli-builder/README.md). Версия закреплена в
[`package.nix`](./package.nix), а [`pnpm-lock.yaml`](./pnpm-lock.yaml) фиксирует dependency graph и
integrity registry-артефактов.

При обновлении измените версию, пересоздайте lock-файл без lifecycle scripts и обновите
`pnpmDepsHash` по ожидаемому hash mismatch Nix. После этого выполните:

```sh
nix flake check --all-systems --no-build
nix build .#packages.x86_64-linux.hydra-acp
```

Hydra остаётся experimental dependency. Обновление считается принятым только после
multi-session/multi-client acceptance из
[f8-06](../../roadmap/f8-pi-runtime/f8-06-network-acp-daemon.md).

## Lattice-патч: `session/list` включает never-prompted сессии

Upstream `0.1.183` скрывает сессии без единого промпта из ACP `session/list` (daemon вызывает
`manager.list` без `includeNonInteractive`). Из-за этого свежесозданная сессия невидима до первого
промпта, и переподключающийся клиент создаёт новую сессию вместо resume — тёплые сессии и агенты
накапливаются молча. Патч в [`package.nix`](./package.nix) (хук `postInstall` общего билдера)
форсирует `includeNonInteractive: !0` на daemon-стороне независимо от клиента. Поведение закреплено
ассертом в [`tests/acp-ingress-smoke.mjs`](../../tests/acp-ingress-smoke.mjs): never-prompted
сессия обязана появиться в `session/list`.

## Lattice-патч (f15-01): серверная политика defaultCwd для `session/new` и `session/list`

Upstream `0.1.183` принимает клиентский cwd как есть: `session/new` создаёт сессию ровно в
присланном клиентом каталоге (схема требует `cwd`), а `session/list` фильтрует список по
присланному клиентом cwd (путь-равенство `Td`/`wo` в `manager.list`). Для node dev-loop это
ломалось с двух сторон: stateless-клиент (acp-ui, Ferngeist) увёл бы новую сессию из рабочего
checkout указанием произвольного пути, а при чтении списка с чужим путём (например `/` после
обновления страницы) скрыл бы все сессии, хотя они и были созданы под `defaultCwd`.

Патч [`package.nix`](./package.nix) делает политику симметричной на сервере: `manager.create`
**безусловно** заменяет cwd на `fe(this.defaultCwd)`, а обработчик `session/list` **безусловно**
листит по `fe(e.manager.defaultCwd)`, игнорируя клиентский путь в обоих случаях. Все ACP-сессии
работают в единой рабочей копии; клиентские патчи (правки acp-ui) не нужны. Регрессия —
`session/list` с cwd = `/` обязан вернуть сессии — закреплена в
[`tests/acp-ingress-smoke.mjs`](../../tests/acp-ingress-smoke.mjs) и
[`tests/hydra-acp-smoke.mjs`](../../tests/hydra-acp-smoke.mjs).
