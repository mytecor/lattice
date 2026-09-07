# Pi package

Здесь находится сборка Pi через общий [pnpm CLI builder](../pnpm-cli-builder/README.md). Имя
`@earendil-works/pi-coding-agent` и версия заданы прямо в `package.nix`; `pnpm-lock.yaml`
закрепляет разрешённый dependency graph и integrity registry-артефактов.

При обновлении Pi измените версию в `package.nix`, пересоздайте `pnpm-lock.yaml` для этой версии и
получите новый `pnpmDepsHash` из ожидаемого Nix hash mismatch. После подстановки хеша выполните:

```sh
nix flake check --all-systems --no-build
nix build .#packages.x86_64-linux.pi
```

Сборка выполняется offline в Nix sandbox; runtime-загрузок из npm registry нет. Lifecycle scripts
отключены `pnpmConfigHook`: upstream Pi не требует их для обычной установки.
