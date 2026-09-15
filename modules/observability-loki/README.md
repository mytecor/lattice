# lattice.observability-loki

NixOS-обёртка над upstream `services.loki` (Grafana Loki single-binary) для
Lattice (F12 observability).

## Что это

Локальное хранилище структурированных событий gateway. Принимает push от
[`lattice.observability-alloy`](../observability-alloy/README.md) на loopback
HTTP `/loki/api/v1/push`, хранит данные в filesystem storage, ретенция 30 дней
(compactor `retention_enabled`), ring — in-memory (одна нода, без memberlist).

## Structured metadata

`limits_config.allow_structured_metadata = true`: Alloy пишет `request_id` и
другие димензии как **поля**, а не лейблы. Поиск по `request_id` работает через
LogQL без раздувания индексной cardinality.

## Песочница

Upstream unit защищён (`ProtectHome`, `ProtectSystem=full`, `DevicePolicy=closed`,
`NoNewPrivileges`, `PrivateTmp`); модуль ничего не переопределяет.

## Ключевые опции

- `listenAddress` / `port` — bind (loopback по умолчанию).
- `dataDir` — `/var/lib/loki` (персистится через /persist).
- `retentionPeriod` — 720h (30 дней).

## Использование

```nix
lattice.observability-loki = {
  enable = true;
  port = 9214;
};
```
