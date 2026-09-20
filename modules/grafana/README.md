# lattice.grafana

NixOS-обёртка над upstream `services.grafana` для Lattice (F12 observability).

## Что это

Frontend-дашборды поверх Loopback Prometheus (числовые метрики) и Loki (расследование
запросов по `request_id`). Датасорсы провижинятся (источник истины — репозиторий),
admin-пароль — из agenix-секрета через file provider (не в Nix store).

## Безопасность

- `http_addr = 127.0.0.1` (loopback), не публичный.
- `admin_password = "${__file:/run/agenix/grafana-admin-password}"` — file provider;
  модуль assertion требует `adminPasswordFile`, refusing безопасного дефолта.
- `secret_key = "${__file:/run/agenix/grafana-secret-key}"` — NixOS 26.05 убрал дефолт; ключ из
  agenix-секрета (`openssl rand -hex 32`), также через file provider.
- `disable_gravatar`, `reporting_enabled=false`, `allow_sign_up=false`.
- Datasources `access=proxy`: пользователи не видят адреса хранилищ; credentials
  ни одного из них не нужны (loopback).

## Dashboard provisioning

`dashboards/` — каталог с dashboard JSON (f12-04, доработка f12-05/f12-06): «LLM Gateway» (`llm-gateway.json`),
«Gateway: модели по провайдерам» (`gateway-providers.json`, динамический разрез `native_model`,
повтор строки по провайдеру), «Gateway runtime» (`gateway-runtime.json`), «Loki / Расследование»
(`loki-investigation.json`). Провайдер `lattice` (папка "Lattice")
подхватывает файлы при старте; definitions живут в репозитории, не в UI.
`dashboardProviders` (если задан) заменяет дефолтный набор целиком — источник истины
один (репозиторий), а не ручная правка в UI.

## Ключевые опции

- `listenAddress` / `port` — bind (loopback).
- `domain` — влияет на `root_url`.
- `adminUser` / `adminPasswordFile` / `secretKeyFile` — admin username + агентским
  secret paths для пароля и secret_key.
- `dataDir` — `/var/lib/grafana` (персистится через /persist).
- `prometheusUrl` / `lokiUrl` — loopback datasource URLs.
- `dashboardProviders` — расширение provisioning providers.

## Использование

```nix
lattice.grafana = {
  enable = true;
  port = 9215;
  prometheusUrl = "http://127.0.0.1:9213";
  lokiUrl = "http://127.0.0.1:9214";
  adminPasswordFile = config.age.secrets.grafana-admin-password.path;
};
```
