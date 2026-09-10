# Pi ACP package

Пакет собирает независимо поддерживаемый
[`regadas/pi-acp`](https://github.com/regadas/pi-acp) из закреплённого Git commit через
`fetchPnpmDeps`, `pnpmConfigHook` и закреплённый [`pnpm-lock.yaml`](./pnpm-lock.yaml). Adapter
переводит ACP JSON-RPC по stdio в уже установленный [`pi`](../pi/README.md) `--mode rpc`; wrapper
добавляет закреплённый Pi в `PATH` без runtime-установок из npm.

При обновлении измените `rev`, дату unstable-версии, source hash и `pnpmDepsHash` в
[`package.nix`](./package.nix). Затем выполните:

```sh
nix flake check --all-systems --no-build
nix build .#packages.x86_64-linux.pi-acp
```

Upstream пока не публикует этот fork в npm или ACP Registry, поэтому Hydra использует локальное
agent definition с абсолютным store-путём, а не динамическую registry-установку.
