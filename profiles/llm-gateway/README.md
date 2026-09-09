## LLM Gateway

Профиль задаёт безопасные network/service defaults для `lattice.llm-gateway`. Runtime — Lattice-owned
Go proxy поверх Bifrost Core.

### Компоненты

- `packages/llm-gateway/` — Go HTTP facade, routing pipeline, catalog и Bifrost execution layer;
- `modules/llm-gateway/` — NixOS module и безопасная сборка runtime config;
- `profiles/llm-gateway/` — loopback port и production-safe defaults.

Профиль оставляет `runtime`/`package` умолчаниям модуля (`pkgs.lattice.llm-gateway`); нода задаёт
providers, logical models (через `models`) и плоские `routingRules`. `inferenceUrl` — это полный путь
до OpenAI-совместимой точки входа (включая версионный сегмент). Если `modelsUrl` не задан, gateway
получает каталог из `${inferenceUrl}/models`; явные `modelsUrl` и `modelsApiKeyFile` позволяют
направить discovery на независимый endpoint.
Полный пример находится в
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
