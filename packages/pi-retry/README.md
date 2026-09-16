# pi-retry

Pi-расширение `@geebos/pi-retry` (форк `pi-retry` от
[narumiruna/pi-extensions](https://github.com/narumiruna/pi-extensions)):
классифицирует provider-specific и stalled-stream ошибки как retryable и
удерживает встроенный агент-ретрай Pi (`stopReason:"error"` + хинт
`provider returned error`).

## Зачем

Pi имеет встроенный агент-ретрай, который матчит ошибки провайдера **по тексту**
`errorMessage` (паттерны `5xx`, `rate limit`, `connection…`, `timed out` и т.п.).
Но сообщение `"upstream stream failed"` — которое llm-gateway шлёт по SSE при
обрыве апстрим-стрима (`server.go`, `event: error`) — **не попадает** ни в один
из встроенных паттернов, поэтому длинный ход умирает без ретрая (регрессия
проверена на `01a0a931…` 2026-09-16: 3 падения `upstream stream failed`, все —
обрыв 5xx от hyperfusion после 100–400s стрима).

`pi-retry` решает это: добавляет **кастомные паттерны** (в
`~/.pi/agent/extensions/pi-retry/config.json`), по которым ошибка помечается
retryable, и позволяется встроенному ретраю продолжить ход с нормальным
backoff.

## Сборка

Полная сборка через общий [`buildPnpmCli`](../pnpm-cli-builder/README.md),
offline в Nix sandbox: `package.nix` задаёт имя/версию (`@geebos/pi-retry`
0.0.2), `pnpm-lock.yaml` закрепляет dependency graph и integrity.
`pnpm-workspace.yaml` отключает pnpm 11 supply-chain политики
(`minimumReleaseAge`, `blockExoticSubdeps`) — для Nix-pinned lock они избыточны.

В отличие от CLI-пакетов, у расширения **нет bin-таргетов**: `executables = { }`
и `versionCheck = false` (installCheckPhase требует CLI-вывод).

Вывод — каталог с двумя симлинк-точками (как у pi-mcp-adapter, без копирования
`node_modules`):

- `extension` — корень установленного пакета (`src/index.ts` — точка входа);
- `node_modules` — полное дерево зависимостей.

Резолвер Pi/Jiti поднимается текстуально от точки входа, поэтому дерево
`node_modules` продублировано симлинком на том же уровне, что и `extension`.

## Подключение

Модуль `lattice.pi.settings.extensions` принимает пакет как значение:

```nix
lattice.pi.settings = {
  extensions = [ pkgs.lattice.pi-mcp-adapter pkgs.lattice.pi-retry ];
  # Паттерны ретрая: "upstream stream failed" ретраябелен (обрыв апстрим-стрима),
  # "unsupported model" — нет (повторный запрос бессмысленен).
  retry = [ "^Provider finish_reason: abort$" "upstream stream failed" ];
};
```

Модуль раскрывает `extensions` в `"${pkg}/extension"` (store-path в
`settings.json`) и материализует `config.json` с паттернами по каноническому
пути `~/.pi/agent/extensions/pi-retry/config.json` (симлинк на store-файл).
На ноде не нужны ни node/npm, ни runtime-загрузки из npm registry.

Управление паттернами вручную — команда `/plugin:retry` (см. README пакета).

## Обновление

1. Обновите `version` в `package.nix`.
2. Пересоздайте `pnpm-lock.yaml` через pnpm версии из nixpkgs
   (`pnpm install --lockfile-only` с `pnpm-workspace.yaml` из этого каталога).
3. Получите новый `pnpmDepsHash` из ожидаемого Nix hash mismatch при сборке.
