## LLM Gateway

Профиль задаёт безопасные network/service defaults для `lattice.llm-gateway`. Runtime — Lattice-owned
Go proxy поверх Bifrost Core.

### Компоненты

- `packages/llm-gateway/` — Go HTTP facade, routing pipeline, catalog и Bifrost execution layer;
- `modules/llm-gateway/` — NixOS module и безопасная сборка runtime config;
- `profiles/llm-gateway/` — loopback port и production-safe defaults.

Профиль оставляет `package` умолчанию модуля (`pkgs.lattice.llm-gateway`) и задаёт только
production-safe defaults в `settings`: loopback listeners, порты, уровень логирования,
catalog/stream intervals и runtime affinity path. Нода задаёт `settings.providers` и плоские
`settings.routing_rules` напрямую в формате gateway JSON. Для OpenAI-compatible adapter
`inference_url` — полный путь до точки входа (включая версионный сегмент), а при отсутствии
`models_url` каталог получается из `${inference_url}/models`. Другим adapters для discovery нужен
явный `models_url`; без него настроенный native target вызывается оптимистично.
Полный пример находится в
[`modules/llm-gateway/README.md`](../../modules/llm-gateway/README.md).

Secrets подаются через agenix → EnvironmentFile: `credentials` связывает явное имя
environment variable с runtime-путём секрета, а `settings` ссылается на него как `env.NAME`.
Client identities находятся в `settings.client_api_keys`; provider credentials — в
`settings.providers[].api_key` или `vertex_auth_credentials`. Метрики запросов/токенов
разбиваются по non-secret `id` клиентского ключа.

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
выведен в mesh с client-auth: `settings.client_api_keys` задаёт per-consumer ключи (включение — при
пересборке ноды), доступ только для доверенных участников yggdrasil-сети.
