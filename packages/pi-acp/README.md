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

## MCP-серверы ACP-сессии

[Патч](./mcp-servers-accepted.patch) передаёт `mcpServers` из
`session/new`, `session/load` и `session/resume` дочернему Pi через
`--mcp-config` и закреплённый [pi-mcp-adapter](./../pi-mcp-adapter/README.md).
Поддерживаются stdio (command/args/env), HTTP и SSE (url/headers).

Каждый процесс получает отдельный временный каталог с правами `0700` и конфиг
с правами `0600`; конфиг удаляется при завершении или ошибке запуска дочернего
процесса. Дескрипторы с токенами не записываются в общий конфиг Pi или карту
ACP-сессий. При восстановлении клиент должен передать актуальные `mcpServers`.

`PI_ACP_DISABLE_BUILTIN_TOOLS=1` отключает встроенные инструменты внутреннего Pi.
Нода задаёт его для `agentrun-openai`, чтобы клиентский `bash` вызывался через
виртуальный MCP, а не подменялся локальным bash внутреннего агента.

[Smoke-тест](./../../tests/pi-acp-smoke.mjs) проверяет реальное подключение
переданного stdio MCP-сервера и запрос `tools/list` без обращения к LLM.
