# lattice.observability-prometheus

NixOS-обёртка над upstream `services.prometheus` для Lattice (F12 observability).

## Что это

Pull-коллектор числовых метрик gateway. Единственный scrape-job по умолчанию —
`llm-gateway` на loopback `/metrics` (по умолчанию `127.0.0.1:9209`, задаётся из
`lattice.llm-gateway.metricsPort`), с константным лейблом `service="llm-gateway"`.
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
- `extraScrapeConfigs` — дополнительные jobs (для будущих /metrics на ноде).

## Использование

```nix
lattice.observability-prometheus = {
  enable = true;
  port = 9213;
};
```
