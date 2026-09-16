# pnpm CLI builder

`buildPnpmCli` собирает npm CLI в Nix store через закреплённые `pnpm`,
`fetchPnpmDeps` и `pnpmConfigHook`. Builder принимает имя npm-пакета, версию, lock-файл и mapping
команд на JS entrypoints, затем генерирует `package.json`, выполняет offline/frozen установку и
создаёт wrappers с закреплённым Node.js.

По умолчанию install-check ожидает точный вывод версии от `<mainProgram> --version`. Для CLI с
другим форматом доступны `versionCheckArgs`, `versionCheckOutput` и `versionCheck = false`.

Минимальный package definition:

```nix
{ buildPnpmCli, lib }:

buildPnpmCli {
  pname = "example-cli";
  version = "1.2.3";
  package = "@example/cli";
  pnpmLock = ./pnpm-lock.yaml;
  pnpmDepsHash = "sha256-...";
  executables.example = "dist/cli.js";
  description = "Example CLI";
  homepage = "https://example.invalid";
  license = lib.licenses.mit;
}
```

Версия остаётся ответственностью package definition. Для нового CLI создаётся собственный
`packages/<name>/package.nix` и lock-файл; общий builder не содержит перечень пакетов.

## Удержание pnpm-deps в Nix store

`fetchPnpmDeps` — это fixed-output derivation (FOD): её выход (`*-pnpm-deps`,
сжатый pnpm-store) `pnpmConfigHook` распаковывает в `node_modules` при сборке,
но **сам FOD ничем не ссылается** в итоговом closure. При `keep-outputs = false`
(по умолчанию) weekly `nix-gc` удаляет такой unreferenced выход, и следующий
`comin`-rebuild заново выполняет `pnpm install --registry=…` против upstream npm
registry по сети — медленно, с ретраями (а `comin` собирает с `--no-link`, не
создавая собственных GC-корней).

Builder решает это автоматически: в `installPhase` записывает store-path
pnpm-deps в `$out/libexec/<pname>/pnpm-deps-store-path`. Так как pnpm-deps уже
является входом derivation (его читает `pnpmConfigHook` из окружения), Nix
переписывает этот путь и фиксирует реальную ссылку из выходного пакета на FOD.
Пока пакет жив в активном closure системы (`/run/current-system`), FOD жив —
внешних GC-корней, списков путей или ручной настройки не требуется; при смене
lock-файла ссылка обновляется на новый FOD автоматически.

Этот же приём применяется в `packages/pi-acp/package.nix`, который использует
`fetchPnpmDeps` напрямую (а не через builder).
