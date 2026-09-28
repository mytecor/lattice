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
получает каталог из `${inferenceUrl}/models`; явный `modelsUrl` позволяет направить discovery на
независимый endpoint (один общий ключ провайдера покрывает и inference, и каталог — отдельного
models-ключа нет).
Полный пример находится в
[`modules/llm-gateway/README.md`](../../modules/llm-gateway/README.md).

Secrets подаются через agenix → EnvironmentFile: ключи достигают процесса **только как
environment-переменные**, в конфиге провайдеров указываются **имена переменных**
(`apiKeyEnv`), а client-ключи задаются списком `clientKeys` (каждый — не-секретный id,
`secretFile` и env-имя). Метрики запросов/токенов разбиваются по id клиентского ключа.

- `clientKeys` — именованные client-ключи Pi/workers (пустой список = keyless);
- `providers.<name>.apiKeySecretFile` — agenix-путь к ключу провайдера;
- `providers.<name>.apiKeyEnv` — имя env-переменной (default `LATTICE_LLM_PROVIDER_<ID>_KEY`).

См. [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md) для bootstrap/rotation workflow.

### Доступ из LAN через mDNS

Если нода также использует `profiles/tcp-gateway` и Avahi, Caddy публикует gateway по
service-specific mDNS hostname:

```text
http://llm-gateway.<node>.local/v1
```

Например: `http://llm-gateway.mytecor-homelab.local/v1`. Caddy проксирует запросы на loopback
listener; порт `9208` напрямую в LAN не открывается.

### Доступ через yggdrasil-mesh

Если на ноде задан `lattice.tcp-gateway.meshDomain` и `llm-gateway` не в `meshExclude`,
тот же backend дополнительно обслуживается на `https://llm-gateway.<meshDomain>/`
(на `mytecor-homelab` — `https://llm-gateway.homelab.myt.su`, с 2026-09-28). Gateway
выведен в mesh с client-auth: `clientKeys` задают per-consumer ключи (включение — при
пересборке ноды), доступ только для доверенных участников yggdrasil-сети.
