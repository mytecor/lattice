## LLM Gateway

Профиль задаёт безопасные network/service defaults для `lattice.llm-gateway`. Целевой runtime —
Lattice-owned Go proxy поверх Bifrost Core; `token_proxy` остаётся включён на существующей homelab
generation до заполнения operational logical-model mappings и прямого cutover.

### Компоненты

- `packages/llm-gateway/` — Go HTTP facade, routing pipeline, catalog и Bifrost execution layer;
- `modules/llm-gateway/` — dual-runtime NixOS module и безопасная сборка runtime config;
- `profiles/llm-gateway/` — loopback port и production-safe defaults.

Для нового runtime задаются `runtime = "bifrost"`, `package = pkgs.lattice.llm-gateway`, providers,
logical models и плоские `routingRules`. Provider `inferenceUrl` не обязан иметь `/v1/models`:
`modelsUrl` и `modelsApiKeyFile` независимы. Полный пример находится в
[`modules/llm-gateway/README.md`](../../modules/llm-gateway/README.md).

Secrets подаются через agenix/systemd credentials:

- `clientCredentialFile` — единый client key Pi/workers;
- `providers.<name>.apiKeyFile` — inference credential;
- `providers.<name>.modelsApiKeyFile` — отдельный discovery credential, если нужен.

См. [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md) для bootstrap/rotation workflow.

### Доступ из LAN через mDNS

Если нода также использует `profiles/tcp-gateway` и Avahi, Caddy публикует gateway по
service-specific mDNS hostname:

```text
http://llm-gateway.<node>.local/v1
```

Например: `http://llm-gateway.mytecor-homelab.local/v1`. Caddy проксирует запросы на loopback
listener; порт `9208` напрямую в LAN не открывается.
