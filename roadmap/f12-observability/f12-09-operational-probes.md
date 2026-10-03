# f12-09. Synthetic probes и эксплуатационное здоровье ноды

Фича: [F12 — Observability](./README.md). Использует alerting из
[f12-07](./f12-07-node-alerting.md); часть метрик зависит от соответствующих сервисов.

**Статус:** ⏳ запланирована; приоритет P2, реализовывать по мере появления source of truth.

## Контекст

Process state `active` не доказывает, что сервис отвечает пользователю. Кроме того, состояние
ноды определяется не только CPU/RAM: важны успешность deploy, свежесть backup, синхронизация
времени, доступность ingress и сетевых/Reticulum путей.

## Что сделать

- [ ] Добавить synthetic HTTP probes для status, Grafana, LLM gateway health и других стабильных
      ingress-контрактов; проверять LAN и, где применимо, mesh path без обхода штатной auth boundary.
- [ ] Добавить service-specific проверки: Prometheus target health, Loki readiness, Caddy config,
      Reticulum/rnsh достижимость и состояние публичных peers.
- [ ] Экспортировать время/результат последнего успешного `comin` apply и возраст применённого
      commit; различать «нового commit нет» и «обновление сломано».
- [ ] После появления штатного backup добавить timestamp/результат последнего успешного backup и
      restore drill; не объявлять наличие файлов в backup-каталоге доказательством восстановления.
- [ ] Добавить NTP synchronization/offset и сетевые packet loss/latency к выбранным стабильным
      endpoints; не использовать публичный endpoint как единственный source of truth.
- [ ] После развёртывания F10/F19 добавить метрики `r1sd`/`containerd` и `agentd`: allocator
      readiness, running/failed workloads и task duration, не вводя фиктивную Lattice queue depth
      и не дублируя application metrics.
- [ ] Добавить dashboard rows и alerts только после определения владельца, нормального диапазона
      и recovery action для каждого сигнала.

## Критерий готовности (Definition of Done)

- [ ] Для каждого probe задокументированы source of truth, interval, timeout, auth boundary,
      failure semantics и конкретное действие оператора.
- [ ] Остановка backend при живом systemd unit детектируется synthetic probe и приводит к alert.
- [ ] Deploy/backup freshness не становится зелёной от одного наличия процесса или файла:
      проверяется завершённая операция с timestamp и результатом.

## Затрагиваемые файлы / слои

- [`profiles/observability`](../../profiles/observability/config.nix) — probes и scrape wiring.
- Модули наблюдаемых сервисов — только для узких health/readiness contracts.
- [`modules/grafana/dashboards`](../../modules/grafana/dashboards/README.md) — operational panels.
- [`tests`](../../tests/README.md) — synthetic failure fixtures и integration contracts.

## Открытые вопросы

- Нужен ли готовый Prometheus blackbox exporter или достаточно service-specific probes.
- Какие внешние endpoints считаются стабильными и кто является владельцем notification/recovery.
