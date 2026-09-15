# lattice.observability-alloy

NixOS-обёртка над upstream `services.alloy` (Grafana Alloy) для Lattice (F12
observability). Читает systemd journal и шлёт структурированные события gateway
в Loki.

## Что это

`loki.source.journal` читает unit `llm-gateway.service` (`matches =
"_SYSTEMD_UNIT=llm-gateway.service"`), `loki.process` парсит JSON-строку события
и кладёт димензии в **structured metadata** (`request_id`, `route`, `status_code`,
`error_type`, `provider`, `model`, `attempts`, `event`), а в лейблы оставляет
только константный `service="llm-gateway"`. `loki.write` пушит в loopback Loki.

Димензии не становятся лейблами: соглашение F12 о низкой cardinality. Поиск по
`request_id` — через structured metadata (см.
[`observability-loki`](../observability-loki/README.md)).

## Песочница

Upstream unit intentionally thin (`DynamicUser=true`,
`SupplementaryGroups=systemd-journal` чтения журнала); модуль добавляет
`ProtectSystem=strict`, `NoNewPrivileges`, `PrivateTmp`, `CapabilityBoundingSet=""`,
`RestrictNamespaces`, umask 0077 и `ReadWritePaths=/var/lib/alloy`.

## Ключевые опции

- `listenAddress` / `port` — Alloy-сервер (debug UI / reload), loopback.
- `journalUnitFilter` — list of `KEY=VALUE` matches (default llm-gateway.service).
- `lokiUrl` — base URL loopback Loki (`/loki/api/v1/push`).
- `structuredMetadataFields` — список полей, переносимых в structured metadata.

## Использование

```nix
lattice.observability-alloy = {
  enable = true;
  port = 9216;
  lokiUrl = "http://127.0.0.1:9214";
};
```
