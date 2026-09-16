# Открытые решения и отложенное

Лента незакрытых вопросов. Сюда переносится незавершённая работа вместо того, чтобы оставаться
в голове. Каждая запись — это открытое решение (ждёт своей фичи) или осознанно отложенная
работа.

## Как устроено

- Запись появляется, когда вопрососознанно откладывается.
- Когда вопрос закрывается, запись переносится в ARCHITECTURE.md / файл фичи / задачу и
  удаляется отсюда.
- Решение №1 (модель сборки) закрыто и зафиксировано в [ARCHITECTURE.md](../ARCHITECTURE.md).
- Инструмент секретов выбран: `agenix`; решение зафиксировано в
  [ARCHITECTURE.md](../ARCHITECTURE.md#секреты).
- Модель идентичности узла закрыта в [f2-02](f2-secrets-identity/f2-02-node-identity.md) и зафиксирована в
  [ARCHITECTURE.md](../ARCHITECTURE.md#идентичность-узла).
- Bootstrap Radicle закрыт в [f4-01](f4-payload/f4-01-radicle-seed-comin.md): начальный config
  приходит из installer checkout или GitHub, затем selective seed получает публичную реплику, а
  `comin` читает локальное bare storage первым remote.
- Решение по LLM gateway runtime закрыто: [f7-06](f7-llm-gateway/f7-06-go-lip-gonka-cutover.md) сохранила
  отрицательный результат Go LIP PoC, а целевой собственный Go proxy поверх Bifrost Go API и
  прямой cutover закреплены в [f7-07](f7-llm-gateway/f7-07-bifrost-go-proxy.md).
- Backend изоляции первого disposable worker зафиксирован в
  [README F10](./f10-disposable-worker/README.md#зафиксированная-архитектура) и
  [f10-01](./f10-disposable-worker/f10-01-package-r1s.md): OCI/`containerd` через r1s `r1sd`,
  общий immutable Pi image и декларативные worker classes. Переход на VM/microVM допустим позднее при
  измеренном требовании к более сильной boundary.

## Открытые решения

4. **Controller storage** — конкретная реализация выбирается в F11 после стабилизации task
   specification и ручного worker lifecycle в F10. Execution backend готов:
   [r1s](https://github.com/mytecor/r1s) (`R1SExecutor`) зафиксирован в
   [f11-03](./f11-controller/f11-03-worker-provisioner.md).
5. **Внешний доступ к сервисам с поддоменов `myt.su` через Yggdrasil** — открыт в
   [f4-05](f4-payload/f4-05-yggdrasil-public-subdomain-ingress.md): хостинг/управление DNS зоны
   `myt.su`, какие сервисы F4 выводятся наружу и каким поддоменам соответствуют.

Смысл вычислений и хранилища уточнён в [f4-03](f4-payload/f4-03-shared-storage-compute.md): вычисления
выполняются disposable workers, общей persistent FS у них нет, caches не являются source of truth,
а S3 хранит artifacts и другие естественно объектные результаты.

Точки входа Reticulum выбраны в [F3-02](f3-reticulum-tcp/f3-02-define-entry-points.md): два публичных
peer, общий реестр в flake и исходящие TCP-соединения.

## Отложенное

1. **Резервный оверлей (Yggdrasil/I2P)** — нужен ли как дополнительный интерфейс Reticulum или
   достаточно нескольких TCP-точек входа. Решение после F3.

2. **Reticulum interface discovery / auto-connect** — проверить поддержку в закреплённом
   `rns-rs`, затем использовать публичные peers как bootstrap для обнаружения других соседей.

3. **Наблюдаемость LLM gateway (метрики + Grafana)** — заведена отдельной вертикалью
   [F12](./f12-observability/README.md), а не follow-up к закрытой F7. Метрики считаются в
   Prometheus-счётчиках (`/metrics`), логи остаются событийными для расследования по
   `request_id`; дашборды — в Grafana. Задачи: [f12-01](./f12-observability/f12-01-gateway-metrics-endpoint.md)
   .. [f12-04](./f12-observability/f12-04-grafana-dashboards.md). OpenTelemetry traces отложены
   до тех пор, пока метрики и логи не покроют реальные вопросы настройки.

4. **Stdio shim для Zed поверх no-auth LAN ACP endpoint** — stock-клиент
   [`@hydra-acp/cli`](../packages/hydra-acp/README.md) несовместим с безаутентичным endpoint
   `ws://acp.<nodename>.local/`: для не-loopback хоста требует credential из `remotes.json`, который
   выдаётся только через `/v1/auth/login`, а daemon без master password отвечает `403`. Caddy host
   при этом переписывает любой path во внутренний `/acp`, так что HTTP API клиента недостижимо в
   принципе (отрицательный результат зафиксирован в
   [f8-06](f8-pi-runtime/f8-06-network-acp-daemon.md#отрицательный-результат-stock-hydra-acp-client-как-local-stdio-shim-для-zed)).
   Открытая работа — auth-задача вместе с LAN boundary: выбрать либо включение Hydra master
   password + пересмотр «host целиком ACP-endpoint», либо собственный минимальный
   stdio→WebSocket shim (форма соединения Ferngeist), который не трогает HTTP API гидры.

   Смежная, но закрытая проблема — разрыв ответов на отдельные чанки из-за per-token `messageId`
   (речь не про Zed): решена на стороне daemon трансформером
   [acp-normalizer](f8-pi-runtime/f8-06-network-acp-daemon.md#трансформер-acp-normalizer-стабильный-messageid-на-логическое-сообщение)
   и включена глобально через `lattice.pi-acp-daemon.defaultTransformers`, см.
   [`packages/acp-normalizer`](../packages/acp-normalizer/README.md).

5. **Вернуть pi-acp-daemon к минимальным доступам (revert privileged)** — на `mytecor-homelab`
   временно включён `lattice.pi-acp-daemon.privileged = true` для живой диагностики
   Wi-Fi/nl80211 (раскрытие `AF_NETLINK`, `CAP_NET_ADMIN`, снятие `NoNewPrivileges`). Это stopgap:
   после завершения диагностики выключить опцию и восстановить строгий песочник (без netlink,
   пустой `CapabilityBoundingSet`, `NoNewPrivileges=true`, `ProtectSystem=full`, `PrivateDevices`).
   Смысл/границы раскрытия — в
   [README модуля](../modules/pi-acp-daemon/README.md#временный-privileged-доступ-stopgap-переработать).
