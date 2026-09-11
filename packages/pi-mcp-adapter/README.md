# pi-mcp-adapter

MCP (Model Context Protocol) adapter extension для Pi: доступ к MCP-серверам через
один proxy tool (`mcp`/`mcpScript`) без раздувания контекста.

## Сборка

Полная сборка через общий [`buildPnpmCli`](../pnpm-cli-builder/README.md),
offline в Nix sandbox: `package.nix` задаёт имя/версию, `pnpm-lock.yaml`
закрепляет dependency graph и integrity артефактов (включая tarball-URL
`pkg.pr.new`). `pnpm-workspace.yaml` отключает pnpm 11 supply-chain политики
(`minimumReleaseAge`, `blockExoticSubdeps`) — для Nix-pinned lock они избыточны.

Вывод — каталог с двумя симлинк-точками (без копирования `node_modules`):

- `extension` — корень установленного пакета (`index.ts` — точка входа, sibling-модули);
- `node_modules` — полное дерево зависимостей.

Резолвер Pi/Jiti поднимается текстуально от данной точки входа, поэтому дерево
`node_modules` продублировано симлинком на том же уровне, что и `extension`.

## Подключение

Модуль `lattice.pi.settings.extensions` принимает этот пакет как значение:

```nix
lattice.pi.settings.extensions = [ pkgs.lattice.pi-mcp-adapter ];
```

Модуль раскрывает его в `"${pkg}/extension"` (store-path в `settings.json`).
На ноде не нужны ни node/npm, ни runtime-загрузки из npm registry.

## Обновление

1. Обновите `version` в `package.nix`.
2. Пересоздайте `pnpm-lock.yaml` через pnpm версии из nixpkgs
   (`pnpm install --lockfile-only` с `pnpm-workspace.yaml` из этого каталога).
3. Получите новый `pnpmDepsHash` из ожидаемого Nix hash mismatch при сборке.
