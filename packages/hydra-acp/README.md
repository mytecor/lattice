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
[f8-06](../../docs/roadmap/f8-pi-runtime/f8-06-network-acp-daemon.md).
