# f12-08. SMART/NVMe, Btrfs integrity и ресурс накопителей

Фича: [F12 — Observability](./README.md). Дополняет системные метрики из
[f12-06](./f12-06-node-system-metrics.md).

**Статус:** ⏳ запланирована; приоритет P1 после базовых алертов.

## Контекст

Заполненность и I/O показывают нагрузку, но не предсказывают деградацию накопителя и не видят
ошибки Btrfs. SMART/NVMe и Btrfs-команды требуют доступа к устройствам или повышенных прав;
основной `node-status` намеренно работает с `PrivateDevices` и пустым capability set.

## Что сделать

- [ ] Инвентаризировать реальные SATA/NVMe устройства homelab и доступные показатели:
      health status, temperature, wear/percentage used, media/data-integrity errors,
      unsafe shutdowns и available spare.
- [ ] Выбрать отдельную privilege boundary: `smartd`/узкий oneshot collector с allowlist
      устройств пишет атомарный textfile, а unprivileged exporter/Prometheus только читает его.
      Не выдавать `node-status` общий доступ к `/dev` или `CAP_SYS_ADMIN`.
- [ ] Экспортировать Btrfs device stats, scrub age/result и allocation data/metadata; ошибки
      checksum/read/write/flush должны быть монотонными counters.
- [ ] Добавить dashboard row для здоровья и ресурса накопителей, без серий на partitions,
      loop/dm temporary devices и других неограниченных именах.
- [ ] Добавить warning/critical rules для failed health, media/Btrfs errors, исчерпания spare,
      wear threshold и просроченного/неуспешного scrub.
- [ ] Проверить поведение при отсутствии SMART, неподдерживаемом атрибуте и временно недоступном
      устройстве: collector сообщает собственную ошибку, но не ломает остальные series.

## Критерий готовности (Definition of Done)

- [ ] На живой ноде видны health/wear/error series для каждого постоянного накопителя и актуальный
      результат Btrfs scrub без расширения sandbox основного `node-status`.
- [ ] Синтетическая fixture с SMART/Btrfs error активирует соответствующее правило; normal fixture
      остаётся inactive.

## Затрагиваемые файлы / слои

- Новый узкий NixOS module/collector для privileged storage facts.
- [`profiles/observability`](../../profiles/observability/config.nix) — включение и scrape/textfile.
- [`modules/grafana/dashboards`](../../modules/grafana/dashboards/README.md) — storage panels.
- [`tests`](../../tests/README.md) — parser, sandbox и generated-artifact contracts.

## Открытые вопросы

- Использовать готовый `smartctl_exporter`, `smartd` + textfile или минимальный собственный
  oneshot; решение принять по доступности в закреплённом nixpkgs и необходимому набору прав.
