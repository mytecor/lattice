# f12-07. Алерты по ресурсам и стабильности сервисов

Фича: [F12 — Observability](./README.md). Зависит от метрик из
[f12-06](./f12-06-node-system-metrics.md).

**Статус:** ⏳ запланирована; приоритет P0 среди follow-up наблюдаемости ноды.

## Контекст

Дашборд помогает расследовать состояние вручную, но не сообщает об аварии без открытой Grafana.
Нужны version-controlled Prometheus rules с устойчивыми окнами, чтобы краткий spike не создавал
шум, а потеря exporter или критичного systemd-сервиса обнаруживалась автоматически.

## Что сделать

- [ ] Добавить recording/alerting rules как декларативный source of truth в репозитории и
      подключить их через NixOS-модуль Prometheus.
- [ ] Алертить отсутствие `node_status_up`, не-active состояние критичного unit и рост
      `node_status_systemd_unit_restarts_total` выше выбранного окна/порога.
- [ ] Добавить sustained alerts для заполнения root filesystem, memory pressure/swap activity,
      высокой CPU/load, температуры и `node_status_collection_errors_total`.
- [ ] Разделить severity минимум на warning/critical; в annotation указывать конкретный unit,
      device или ресурс и ссылку на dashboard, но не секреты и не high-cardinality данные.
- [ ] Выбрать и декларативно подключить notification route. До выбора канала правила должны
      быть видны в Prometheus/Grafana, но не отправлять уведомления в случайный внешний сервис.
- [ ] Добавить тесты семантики rules: нормальное состояние, sustained threshold, восстановление,
      отсутствие target и restart burst.

## Критерий готовности (Definition of Done)

- [ ] Искусственно остановленный тестовый unit и недоступный exporter переводят ожидаемые alerts
      в firing после заданного `for`, а после восстановления alerts возвращаются в inactive.
- [ ] Пороговые resource alerts проверены синтетическими series; кратковременные пики не создают
      уведомление.
- [ ] На живой ноде rules загружены без ошибок и минимум один безопасный тестовый alert прошёл
      полный путь до выбранного notification receiver.

## Затрагиваемые файлы / слои

- [`modules/observability-prometheus`](../../modules/observability-prometheus/README.md) — rules
  и provisioning.
- [`modules/grafana`](../../modules/grafana/README.md) — отображение alert state и data links.
- [`profiles/observability`](../../profiles/observability/config.nix) — node defaults.
- [`tests`](../../tests/README.md) — rule contracts и smoke-проверки.

## Открытые вопросы

- Куда слать уведомления: локальный канал, email, Telegram/ACP или несколько receiver по severity.
- Пороговые значения надо выбрать после минимум недели baseline-данных с живой ноды; сначала
  фиксируем очевидные safety thresholds и отсутствие сервисов.
