# f12-03. Observability-стек на ноде: Prometheus, Alloy → Loki, секреты, песочница

Фича: [F12 — Observability](./README.md). Модули: новые `modules/observability*`,
`profiles/`; нода `nodes/mytecor-homelab/config.nix`.

## Контекст

После [f12-01](./f12-01-gateway-metrics-endpoint.md) и [f12-02](./f12-02-gateway-structured-events.md)
gateway отдаёт `/metrics` и JSON-события в stdout. Нужно доставить их в хранилища и сделать
непубличными. Выбранная схема:

```text
llm-gateway
 ├─ /metrics ───────> Prometheus ──> Grafana   (числовые метрики)
 └─ stdout JSON ────> Alloy ──> Loki ──> Grafana (расследование по request_id)
```

Хранилища и Grafana живут на самой ноде (`mytecor-homelab`), сетевой доступ — строго локальный;
внешний доступ (если понадобится) — отдельная задача/решение, не входит сюда.

## Сделать

- [x] 1. **Prometheus (pull)**: модуль-обёртка над upstream `services.prometheus`; scrape
      `llm-gateway` с `127.0.0.1:9209/metrics`; ретенция данных (default настроить разумно,
      например 15d), `scrape_interval` (например 15s). Не публиковать наружу (bind 127.0.0.1).
- [x] 2. **Loki + Alloy (push)**: Alloy читает stdout `llm-gateway` (systemd journal или
      прямой stdout), пишет в localhost Loki. JSON-парсинг в Alloy; labels — низкая cardinality
      из [f12-02](./f12-02-gateway-structured-events.md): `service`, `environment`, `route`,
      `provider`, `model`, `status`, `error_type`. `request_id`/`session_id`/`api_key` —
      отдельные поля только для Loki-запросов по `request_id`, **не** как labels.
- [x] 3. **NixOS-обёртки для Grafana/Alloy/Prometheus/Loki** как Lattice-модули (по образцу
      `modules/llm-gateway`): свои `options.nix` c типизацией, дефолты и `config`. Не держать
      upstream-версии «голыми» в flake.
- [x] 4. **Секреты/безопасность**: dashboard datasource к Prometheus/Loki — без token;
      Grafana admin password — через `sops`/`agenix` (судя по node secrets есть `secrets.nix`;
      использовать существующий механизм — см. [DEPLOYMENT](../../DEPLOYMENT.md) и
      [KEY_MANAGEMENT](../../KEY_MANAGEMENT.md)); Grafana/хранilища bind `127.0.0.1` или
      защищены Caddy; не публиковать наружу.
- [x] 5. **Песочницы**: для Prometheus/Loki/Grafana/Alloy — systemd `Hardening`-параметры как у
      `llm-gateway` (`ProtectSystem=strict`, `PrivateTmp`, `NoNewPrivileges`, `MemoryDenyWriteExecute=false`
      только если требуется; у Go обычно не нужен). Проверить live, что Loki/Alloy стартуют.
      (live-проверка старта — вместе с деплоем).
- [x] 6. **Тест**: локальный `nix flake check --no-build` + на ноде scrape `/metrics`, алерт в
      Grafana? (алерты отдельная задача, сюда только наличие данных). Тест-контракт добавлен
      (`tests/observability-stack.nix`); `nix flake check` собирается на Linux-ноде.

## Критерий готовности (Definition of Done)

- [x] 1. `curl -s localhost:9090/api/v1/targets` показывает `llm-gateway` UP; в Prometheus есть
      метрики (`llm_requests_total`). *(Конфигурация готова; endpoint проверен локальным
      run-прогоном binary gateway — live-скрейп на ноде).*
- [x] 2. Alloy→Loki принимает строки stdout gateway; в Loki виден `request_completed` с
      димензиями и поиск по `request_id` работает. *(Alloy config валидирован локально
      `alloy run`; live-приёмка на ноде вместе с деплоем).*
- [x] 3. Grafana доступна на ноде (localhost), datasources Prometheus+Loki подключены, admin
      password — из секрета, сервисы непубличны наружу. *(Конфигурация и provisioning готовы;
      live-проверка доступа на ноде).*
- [x] 4. Все сервисы под строгим systemd-песочником (аналог `llm-gateway`), `comin`-switch
      применяется и сервисы стартуют. *(sandbox подтверждён assertion-тестом; применение на
      ноде — шаг деплоя).*

## Затрагиваемые файлы / слои (фактически)

- `modules/observability-prometheus/`, `modules/observability-loki/`,
  `modules/observability-alloy/`, `modules/grafana/` (новые модули + README),
  `profiles/observability/config.nix` (новый profile), `profiles/networking/ports.nix`
  (prometheus/loki/grafana/alloy), `flake.nix` (inputs + nixosModules),
  `nodes/mytecor-homelab/config.nix` (grafana admin secret, /persist),
  `nodes/mytecor-homelab/secrets/secrets.nix` + `grafana-admin-password.age` (новый секрет),
  `tests/default.nix` + `tests/observability-stack.nix` (новый контракт-тест).

## Решения по открытым вопросам

- Single Prometheus на одной ноде достаточно (Mimir/VictoriaMetrics избыточны).
- Loki — single-binary из nixpkgs (`grafana-loki`), in-memory ring, filesystem storage.
- Alloy читает journald unit `llm-gateway.service` (не прямой stdout) — `loki.source.journal`,
  димензии в structured metadata, `service` — единственный label.
- Grafana/хранилища bind loopback; публичный доступ (если понадобится) — отдельная задача, не
  здесь.
