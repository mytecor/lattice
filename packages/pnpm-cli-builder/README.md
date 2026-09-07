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
