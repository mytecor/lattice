# f12-06. Go node-status и системные метрики ноды

Фича: [F12 — Observability](./README.md). Follow-up к исходному статическому status endpoint из
[f4-04](../f4-payload/f4-04-enrich-node-status.md).

**Статус:** 🟡 реализация готова 2026-09-29, требуется live-приёмка после deploy.

## Контекст

Прежний `node-status` был JSON-файлом, который activation script генерировал при переключении
NixOS, а Caddy отдавал через `file_server`. Он показывал generation/commit/kernel, но не позволял
наблюдать нагрузку, исчерпание ресурсов и нестабильность сервисов во времени.

Цель задачи — сохранить простой status API, но сделать его Lattice-owned Go-сервисом и источником
низко-cardinality Prometheus-метрик для существующего стека Prometheus/Grafana.

## Что сделано

- [x] Статический writer заменён Go-пакетом
      [`packages/node-status`](../../packages/node-status/README.md); старый shell writer удалён.
- [x] Сохранён JSON-контракт `/` с `node`, `service`, `generation`, `commit`, `kernel`,
      `stateVersion`, `activatedAt`; добавлен объект `system` с текущим состоянием.
- [x] Generation, applied commit и время активации читаются при каждом запросе, поэтому
      очередной `comin` switch не требует обязательного рестарта exporter для обновления metadata.
- [x] Добавлены `/healthz` и `/metrics` в Prometheus text exposition format.
- [x] Собираются CPU time/utilization, load average, RAM/swap, uptime, root filesystem,
      disk I/O, network traffic/errors/drops, thermal zones, длительность и ошибки сбора.
- [x] Для ограниченного списка systemd-юнитов экспортируются текущее состояние и число
      рестартов: Caddy, comin, LLM gateway, Prometheus, Loki, Alloy, Grafana, rns-server, rnsh.
- [x] Сервис слушает только `127.0.0.1:9217`; Caddy проксирует status API, но отвечает `404`
      на внешний `/metrics`. Backend-порт не открыт в firewall.
- [x] Systemd unit запускается с `DynamicUser`, `NoNewPrivileges`, `ProtectSystem=strict`,
      `PrivateDevices`, пустым capability set и ограниченными address families.
- [x] В [`observability-prometheus`](../../modules/observability-prometheus/README.md) добавлен
      отдельный `node-status` scrape job с постоянными `service` и `environment` labels.
- [x] Добавлен provisioning-дашборд
      [`Node overview`](../../modules/grafana/dashboards/node-overview.json): service state/restarts,
      CPU, load, memory, root filesystem, uptime, disk/network throughput, температуры и ошибки.
- [x] Добавлены Go unit/runtime tests и Nix contract tests; `go test -race`, `go vet`,
      `nix flake check --all-systems --no-build` и `lychee` проходят локально.

## Что осталось

- [ ] Опубликовать изменения по штатной процедуре из
      [DEPLOYMENT.md](../../DEPLOYMENT.md#публикация-в-radicle-и-github) и дождаться применения
      `comin` на `mytecor-homelab`.
- [ ] Подтвердить на ноде `lattice-node-status.service` active и отсутствие restart loop.
- [ ] Проверить с LAN-клиента JSON `/` и `/healthz`; внешний `/metrics` обязан вернуть `404`.
- [ ] Проверить loopback `/metrics`, Prometheus target `node-status` в состоянии UP и появление
      данных во всех применимых панелях `Node overview`.
- [ ] Убедиться, что unprivileged hardened unit читает systemd state, `/proc`, `/sys/block` и
      доступные thermal zones; отсутствие сенсора должно давать отсутствие серии, а не падение.

## Критерий готовности (Definition of Done)

- [x] Пакет, NixOS-интеграция, scrape job, dashboard provisioning и contract tests находятся
      в репозитории и проходят evaluation.
- [ ] На живой ноде Prometheus target UP, JSON показывает актуальный generation/commit, а
      дашборд содержит реальные resource и service-stability series минимум за один час.

## Затрагиваемые файлы / слои

- [`packages/node-status`](../../packages/node-status/README.md) — Go API/exporter и тесты.
- [`profiles/app-services`](../../profiles/app-services/README.md) — systemd unit, Caddy ingress,
  mDNS и sandbox.
- [`modules/observability-prometheus`](../../modules/observability-prometheus/README.md) — scrape.
- [`modules/grafana/dashboards`](../../modules/grafana/dashboards/README.md) — dashboard.
- [`tests`](../../tests/README.md) — package/runtime и generated-artifact contracts.

## Архитектурные ограничения

- Сам `node-status` не хранит time series: историю хранит Prometheus.
- Метрики не содержат request id, IP, commit и другие unbounded labels; build metadata — одна
  info-series на target.
- SMART, Btrfs admin-команды и другие privileged collectors не должны расширять capability set
  основного exporter; для них предусмотрена отдельная задача [f12-08](./f12-08-storage-health.md).
