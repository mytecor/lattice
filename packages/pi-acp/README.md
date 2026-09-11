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

## Patch: `mcpServers` принимается (`mcp-servers-accepted.patch`)

Базовый `regadas/pi-acp` отклоняет любой `session/new`/`load`/`resume` с непустым `mcpServers`
(`MCP_SERVERS_UNSUPPORTED`), потому что pi не умеет MCP «нативно». Исходный upstream
([`svkozak/pi-acp`](https://github.com/svkozak/pi-acp)) принимает поле и хранит его в session
state: MCP-инструменты в сессии даёт pi-mcp-adapter, настроенный на хосте — к ACP-транспорту это
не относится.

Патч приводит поведение к upstream-виду: `mcpServers` принимается, пишется warning (сервера из
запроса не подключаются на этом слое), а capability `mcpCapabilities: { http, sse }` объявляется.
Реальные MCP-сервера настраиваются на хосте (`lattice.pi.mcp` → `.mcp.json` /
`~/.config/mcp/mcp.json` / `<pi agent dir>/mcp.json`) и попадают в сессию через pi-mcp-adapter.

Регрессия — в [`tests/pi-acp-smoke.mjs`](../../tests/pi-acp-smoke.mjs): `session/new` с непустым
`mcpServers` должен проходить.
