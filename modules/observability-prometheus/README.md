# lattice.observability-prometheus

NixOS-обёртка над upstream `services.prometheus` для Lattice (F12 observability).

## Что это

Pull-коллектор числовых метрик. Scrape jobs по умолчанию: `llm-gateway` на
`127.0.0.1:9209/metrics` и `node-status` на `127.0.0.1:9217/metrics`, с постоянными лейблами
`service` и `environment`.
Ретенция 15 дней, scrape interval 15s; всё слушает `127.0.0.1` — не публично.

## Песочница

Upstream unit уже предельно жёсткий (`ProtectSystem=full`, `PrivateUsers`,
`DevicePolicy=strict`, `MemoryDenyWriteExecute=true`, `SystemCallFilter`); модуль
ничего не переопределяет.

## Ключевые опции

- `listenAddress` / `port` — bind (loopback по умолчанию).
- `retentionTime` — 15d.
- `scrapeInterval` / `evaluationInterval` — 15s.
- `stateDir` — `/var/lib/prometheus` (персистится нодой через /persist).
- `nodeStatusPort` — loopback-порт Go exporter (по умолчанию 9217).
- `extraScrapeConfigs` — дополнительные jobs (для будущих /metrics на ноде).

## Использование

```nix
lattice.observability-prometheus = {
  enable = true;
  port = 9213;
};
```
