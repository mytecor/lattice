# Роадмап Lattice

Верхнеуровневый план и детали живут в разных файлах, чтобы читать можно было с нужной глубины, не
погружаясь глубоко в проект. Каждая фича — отдельная вертикаль со своим каталогом
`roadmap/<feature-id>-<feature-slug>/`, где лежат файл фичи (`README.md`) и её задачи. Шаблоны для
заведения новых features и tasks лежат в [TEMPLATE_FEATURE.md](./roadmap/TEMPLATE_FEATURE.md) и
[TEMPLATE_TASK.md](./roadmap/TEMPLATE_TASK.md).

Приоритет задаётся положением в этом файле: чем выше фича в списке, тем раньше за неё браться.
Номера фич — стабильные идентификаторы: при переупорядочивании и заведении новых фич они не
меняются и порядок выполнения не кодируют. Закрытые фичи опускаются в «Выполненные» и стоят там
в порядке закрытия — чем раньше закрыта, тем выше. Открытые решения и отложенное — в
[BACKLOG.md](./roadmap/BACKLOG.md).

## Очередь (приоритет сверху вниз)

### [F10. Disposable worker](./roadmap/f10-disposable-worker/README.md)

> Задача выполняется в одноразовом окружении. Execution backend — готовый
> [r1s](https://github.com/mytecor/r1s) (`r1sd`-allocator над `containerd`); временный `LocalExecutor`
> из плана убран.

- **Статус:** 🟡 начата — [f10-01](./roadmap/f10-disposable-worker/f10-01-package-r1s.md) (упаковка
  execution backend r1s/r1sd) закрыта 2026-09-16; следующим — разворачивание `r1sd`-allocator
  на ноде ([f10-02](./roadmap/f10-disposable-worker/f10-02-deploy-r1sd.md)), затем общий immutable
  Pi image и контейнерный Pi runtime (f10-04), worker credentials (f10-05) и acceptance (f10-06).
  Что именно Lattice фиксирует поверх r1s (task spec, lifecycle) решается по ходу.
- **Готово, когда:** полный цикл завершается результатом после уничтожения worker.
- **Зависит от:** [F8](#f8-интерактивный-pi), [F9](#f9-caches-и-artifacts)
  (только caches-часть; artifacts/S3 отложена)

### [F11. Controller](./roadmap/f11-controller/README.md)

> Автоматизация очереди и жизненного цикла workers.

- **Статус:** ⏳ ещё не начата — ждёт стабилизации task specification и ручного worker
  lifecycle в F10.
- **Готово, когда:** задача переживает сбой controller/worker без потери или двойной публикации
  результата.
- **Зависит от:** [F10](#f10-disposable-worker)

### [F4. Полезная нагрузка](./roadmap/f4-payload/README.md)

> Нода становится self-hosted средой и source origin.

- **Статус:** 🟡 частично — source bootstrap (f4-01) и app services (f4-02, f4-03, f4-04)
  работают на homelab; drill без GitHub, полный bootstrap новой ноды и f4-05
  (Yggdrasil-ingress) отложены на потом.
- **Готово, когда:** конфиг распространяется без GitHub; на ноде работает прикладной сервис.
- **Не блокирует:** [F10](#f10-disposable-worker), [F11](#f11-controller) — зависят только
  от f4-01 (Radicle seed/comin), который выполнен.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

### [F9. Caches и artifacts](./roadmap/f9-cache-artifact-plane/README.md)

> Ускорение отделено от ценного результата.

- **Статус:** 🟡 частично — caches-часть (f9-01..f9-03) выполнена и live-подтверждена 2026-09-14;
  artifacts/S3 намеренно отложена на сильно потом (не блокирует F10/F11).
- **Готово, когда:** caches можно удалить без потери корректности, artifacts сохраняются отдельно.
- **Не блокирует:** [F10](#f10-disposable-worker), [F11](#f11-controller).
- **Зависит от:** [F4-01](./roadmap/f4-payload/f4-01-radicle-seed-comin.md), [F7](#f7-llm-gateway),
  [F8](#f8-интерактивный-pi)

### [F5. Внешние узлы](./roadmap/f5-external-nodes/README.md)

> Чистая граница узла и политика доверия.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** узел из внешнего репозитория участвует в сети без доступа к чужим секретам.
- **Зависит от:** [F2](#f2-секреты-и-идентичность), [F3](#f3-reticulum-поверх-tcpip),
  [F4](#f4-полезная-нагрузка)

### [F13. Web-клиент ACP](./roadmap/f13-acp-web-client/README.md)

> Второй ACP-клиент (браузерный workbench acp-components) против существующего LAN endpoint.

- **Статус:** 🟡 начата — [f13-01](./roadmap/f13-acp-web-client/f13-01-deploy-acp-components.md)
  (разворачивание [`acp-components`](https://github.com/zvzuola/acp-components) против LAN
  ACP endpoint из [f8-06](#f8-интерактивный-pi)).
- **Готово, когда:** клиент развёрнут декларативно, подключается к `ws://acp.<nodename>.local/`
  и воспроизводит acceptance из [f8-06](#f8-интерактивный-pi) — либо зафиксирован
  воспроизводимый отрицательный результат совместимости.
- **Зависит от:** [F8](#f8-интерактивный-pi)
- **Не блокирует:** [F10](#f10-disposable-worker), [F11](#f11-controller).

### [F6. Радио и mesh](./roadmap/f6-radio-mesh/README.md)

> Работа на узком канале.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** два узла обмениваются данными по радио при отключённом интернете.
- **Зависит от:** [F3](#f3-reticulum-поверх-tcpip)

## Выполненные (в порядке закрытия)

### [F2. Секреты и идентичность](./roadmap/f2-secrets-identity/README.md)

> Безопасность на ключах, а не на закрытости.

- **Статус:** ✅ выполнена 2026-09-04
- **Готово, когда:** репозиторий можно опубликовать целиком — доступа к узлам он не даёт.
- **Зависит от:** [F1](#f1-одна-железная-нода)

### [F3. Reticulum поверх TCP/IP](./roadmap/f3-reticulum-tcp/README.md)

> Узлы связываются по интернету, не только в LAN.

- **Статус:** ✅ выполнена 2026-09-05
- **Готово, когда:** с ноутбука открывается shell на узле за NAT через rnsh, связь переживает смену
  IP.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

### [F8. Интерактивный Pi](./roadmap/f8-pi-runtime/README.md)

> Основной harness работает непосредственно на ноде.

- **Статус:** ✅ выполнена 2026-09-11 — интерактивная работа идёт через ACP, Pi TUI не
  используется; execution boundary для F10 задаёт контейнерный Pi runtime из f10-04. Follow-up
  [f8-07](./roadmap/f8-pi-runtime/f8-07-telegram-acprouter.md) (Telegram-клиент ACP через
  `vcoderun/acprouter` против закреплённого endpoint f8-06) заведён 2026-09-18, ещё не начат.
- **Готово, когда:** Pi TUI выполняет реальную задачу через gateway и воспроизводимый набор tools.
- **Зависит от:** [F7](#f7-llm-gateway)

### [F7. LLM gateway](./roadmap/f7-llm-gateway/README.md)

> Единая точка доступа к моделям и provider credentials.

- **Статус:** ✅ выполнена 2026-09-14 (f7-01..f7-13, включая provider balancing с live-прогоном);
  follow-up f7-14 (декларативные модели + p2c) реализован 2026-09-16. Дальнейшая наблюдаемость
  (метрики `/metrics`, structured события, Grafana) вынесена в
  [F12](#f12-observability-метрики-gateway--grafana), а не в follow-up закрытой F7.
- **Готово, когда:** клиенты используют только логические классы моделей, а отказ upstream
  обрабатывается заданной политикой.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

### [F1. Одна железная нода](./roadmap/f1-one-node/README.md)

> Проект перестаёт быть только проектом.

- **Статус:** ✅ выполнена 2026-09-16 (закрыт последний пункт f1-01: профили реально
  попадают в сборку — подтверждено CI `checks.x86_64-linux.example` и живой нодой
  `mytecor-homelab`, которая применяет `main` через comin с маркерами `profiles/base`).
- **Готово, когда:** нода ставится с нуля по документации, переживает перезагрузку и сама применяет
  коммит из `main`.
- **Зависит от:** —

### [F12. Observability (метрики gateway + Grafana)](./roadmap/f12-observability/README.md)

> Метрики, структурированные события и дашборды для декларативной настройки LLM gateway.

- **Статус:** ✅ выполнена 2026-09-16 —
  [f12-01](./roadmap/f12-observability/f12-01-gateway-metrics-endpoint.md)
  и [f12-02](./roadmap/f12-observability/f12-02-gateway-structured-events.md)
  реализованы 2026-09-15 (`/metrics` + отдельный loopback-листенер;
  структурированные JSON-события request/attempt);
  [f12-03](./roadmap/f12-observability/f12-03-observability-stack.md)
  (observability stack: Prometheus/Loki/Alloy/Grafana) реализована 2026-09-16;
  [f12-04](./roadmap/f12-observability/f12-04-grafana-dashboards.md) (Grafana-дашборды
  «LLM Gateway», «Gateway runtime», «Loki / Расследование») реализована 2026-09-16;
  [f12-05](./roadmap/f12-observability/f12-05-dashboard-polish.md) (доработка дашбордов: status,
  p50/p99, data links, версия сборки + фикс утечки `llm_requests_in_flight` и фильтра
  `request_id`) реализована 2026-09-16.
- **Готово, когда:** числовые метрики (`/metrics` → Prometheus) и JSON-события (stdout → Alloy /
  Loki) видны в Grafana; конкретный запрос связывается по `request_id` до переходов в
  retry/fallback/race; димензии низкой cardinality, без публичных сервисов.
- **Зависит от:** [F7](#f7-llm-gateway)
