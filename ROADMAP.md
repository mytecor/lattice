# Роадмап Lattice

Верхнеуровневый план и детали живут в разных файлах, чтобы читать можно было с нужной глубины, не
погружаясь глубоко в проект. Каждая фича — отдельная вертикаль со своим каталогом
`roadmap/<feature-id>-<feature-slug>/`, где лежат файл фичи (`README.md`) и её задачи. Шаблоны для
заведения новых features и tasks лежат в [TEMPLATE_FEATURE.md](./roadmap/TEMPLATE_FEATURE.md) и
[TEMPLATE_TASK.md](./roadmap/TEMPLATE_TASK.md).

Номера фич — стабильные идентификаторы, а не требование выполнять все строки сверху вниз.
Открытые решения и отложенное — в [BACKLOG.md](./roadmap/BACKLOG.md).

## [F1. Одна железная нода](./roadmap/f1-one-node/README.md)

> Проект перестаёт быть только проектом.

- **Статус:** ✅ выполнена 2026-09-16 (закрыт последний пункт f1-01: профили реально
  попадают в сборку — подтверждено CI `checks.x86_64-linux.example` и живой нодой
  `mytecor-homelab`, которая применяет `main` через comin с маркерами `profiles/base`).
- **Готово, когда:** нода ставится с нуля по документации, переживает перезагрузку и сама применяет
  коммит из `main`.
- **Зависит от:** —

## [F2. Секреты и идентичность](./roadmap/f2-secrets-identity/README.md)

> Безопасность на ключах, а не на закрытости.

- **Статус:** ✅ выполнена
- **Готово, когда:** репозиторий можно опубликовать целиком — доступа к узлам он не даёт.
- **Зависит от:** [F1](#f1-одна-железная-нода)

## [F3. Reticulum поверх TCP/IP](./roadmap/f3-reticulum-tcp/README.md)

> Узлы связываются по интернету, не только в LAN.

- **Статус:** ✅ выполнена
- **Готово, когда:** с ноутбука открывается shell на узле за NAT через rnsh, связь переживает смену
  IP.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

## [F4. Полезная нагрузка](./roadmap/f4-payload/README.md)

> Нода становится self-hosted средой и source origin.

- **Статус:** 🟡 частично — source bootstrap (f4-01) и app services (f4-02, f4-03, f4-04)
  работают на homelab; drill без GitHub, полный bootstrap новой ноды и f4-05
  (Yggdrasil-ingress) отложены на потом.
- **Готово, когда:** конфиг распространяется без GitHub; на ноде работает прикладной сервис.
- **Не блокирует:** [F10](#f10-disposable-worker), [F11](#f11-controller) — зависят только
  от f4-01 (Radicle seed/comin), который выполнен.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

## [F5. Внешние узлы](./roadmap/f5-external-nodes/README.md)

> Чистая граница узла и политика доверия.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** узел из внешнего репозитория участвует в сети без доступа к чужим секретам.
- **Зависит от:** [F2](#f2-секреты-и-идентичность), [F3](#f3-reticulum-поверх-tcpip),
  [F4](#f4-полезная-нагрузка)

## [F6. Радио и mesh](./roadmap/f6-radio-mesh/README.md)

> Работа на узком канале.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** два узла обмениваются данными по радио при отключённом интернете.
- **Зависит от:** [F3](#f3-reticulum-поверх-tcpip)

## [F7. LLM gateway](./roadmap/f7-llm-gateway/README.md)

> Единая точка доступа к моделям и provider credentials.

- **Статус:** ✅ выполнена (включая f7-13 provider balancing, live-прогон 2026-09-14).
  Дальнейшая наблюдаемость (метрики `/metrics`, structured события, Grafana) вынесена в
  [F12](#f12-observability-метрики-gateway-графана), а не в follow-up закрытой F7.
- **Готово, когда:** клиенты используют только логические классы моделей, а отказ upstream
  обрабатывается заданной политикой.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

## [F8. Интерактивный Pi](./roadmap/f8-pi-runtime/README.md)

> Основной harness работает непосредственно на ноде.

- **Статус:** ✅ выполнена
- **Готово, когда:** Pi TUI выполняет реальную задачу через gateway и воспроизводимый набор tools.
- **Зависит от:** [F7](#f7-llm-gateway)

## [F9. Caches и artifacts](./roadmap/f9-cache-artifact-plane/README.md)

> Ускорение отделено от ценного результата.

- **Статус:** 🟡 частично — caches-часть (f9-01..f9-03) выполнена и live-подтверждена; artifacts/S3
  намеренно отложена на сильно потом (не блокирует F10/F11).
- **Готово, когда:** caches можно удалить без потери корректности, artifacts сохраняются отдельно.
- **Не блокирует:** [F10](#f10-disposable-worker), [F11](#f11-controller).
- **Зависит от:** [F4-01](./roadmap/f4-payload/f4-01-radicle-seed-comin.md), [F7](#f7-llm-gateway),
  [F8](#f8-интерактивный-pi)

## [F10. Disposable worker](./roadmap/f10-disposable-worker/README.md)

> Задача выполняется в одноразовом окружении. Execution backend — готовый
> [r1s](https://github.com/mytecor/r1s) (`r1sd`-allocator над `containerd`); временный `LocalExecutor`
> из плана убран.

- **Статус:** 🟡 начата — [f10-01](./roadmap/f10-disposable-worker/f10-01-package-r1s.md) (упаковка
  execution backend r1s/r1sd) закрыта 2026-09-16; остальные задачи f10-* впереди.
- **Готово, когда:** полный цикл завершается результатом после уничтожения worker.
- **Зависит от:** [F8](#f8-интерактивный-pi), [F9](#f9-caches-и-artifacts)
  (только caches-часть; artifacts/S3 отложена)

## [F11. Controller](./roadmap/f11-controller/README.md)

> Автоматизация очереди и жизненного цикла workers.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** задача переживает сбой controller/worker без потери или двойной публикации
  результата.
- **Зависит от:** [F10](#f10-disposable-worker)

## [F12. Observability (метрики gateway + Grafana)](./roadmap/f12-observability/README.md)

> Метрики, структурированные события и дашборды для декларативной настройки LLM gateway.

- **Статус:** ✅ выполнена 2026-09-16 —
  [f12-01](./roadmap/f12-observability/f12-01-gateway-metrics-endpoint.md)
  и [f12-02](./roadmap/f12-observability/f12-02-gateway-structured-events.md)
  реализованы 2026-09-15 (`/metrics` + отдельный loopback-листенер;
  структурированные JSON-события request/attempt);
  [f12-03](./roadmap/f12-observability/f12-03-observability-stack.md)
  (observability stack) реализована 2026-09-16;
  [f12-04](./roadmap/f12-observability/f12-04-grafana-dashboards.md) (дашборды)
  реализована 2026-09-16.
- **Готово, когда:** числовые метрики (`/metrics` → Prometheus) и JSON-события (stdout → Alloy /
  Loki) видны в Grafana; конкретный запрос связывается по `request_id` до переходов в
  retry/fallback/race; димензии низкой cardinality, без публичных сервисов.
- **Зависит от:** [F7](#f7-llm-gateway)
- **Задачи:** [f12-01](./roadmap/f12-observability/f12-01-gateway-metrics-endpoint.md),
  [f12-02](./roadmap/f12-observability/f12-02-gateway-structured-events.md),
  [f12-03](./roadmap/f12-observability/f12-03-observability-stack.md),
  [f12-04](./roadmap/f12-observability/f12-04-grafana-dashboards.md)

## Текущий порядок реализации

- [F12](#f12-observability-метрики-gateway-графана) — **выполнена 2026-09-16**:
  [f12-01](./roadmap/f12-observability/f12-01-gateway-metrics-endpoint.md) (`/metrics` +
  loopback-листенер) реализован 2026-09-15, [f12-02](./roadmap/f12-observability/f12-02-gateway-structured-events.md)
  (structured JSON-события request/attempt) реализован; [f12-03](./roadmap/f12-observability/f12-03-observability-stack.md)
  (observability stack: Prometheus/Loki/Alloy/Grafana) реализован 2026-09-16;
  [f12-04](./roadmap/f12-observability/f12-04-grafana-dashboards.md) (Grafana-дашборды
  «LLM Gateway», «Gateway runtime», «Loki / Расследование») реализован 2026-09-16.
- [F9](#f9-caches-и-artifacts) — caches-часть (f9-01..f9-03) выполнена и закрыта
  live-прогоном 2026-09-14; artifacts/S3-часть отложена на сильно потом (сознательно
  не блокирует F10/F11).
- [F7](#f7-llm-gateway) — выполнена 2026-09-14, включая f7-13 provider balancing
  (live-прогон на homelab, round_robin + `race count = 1`).
- [F8](#f8-интерактивный-pi) — выполнена: интерактивная работа идёт через ACP, Pi TUI
  не используется; execution boundary для F10 задаёт контейнерный Pi runtime из f10-04.
- [F2](#f2-секреты-и-идентичность), [F3](#f3-reticulum-поверх-tcpip) — выполнены
  (секреты/идентичность; rnsh за NAT через публичную сеть).
- [F1](#f1-одна-железная-нода) — **выполнена 2026-09-16**: последний пункт f1-01
  («проверить, что профили попадают в сборку») закрыт — профили подтверждены в
  CI (`checks.x86_64-linux.example`; asserts про `profiles/base` в `tests/default.nix`
  задокументированы как guard f1-01) и на живой ноде (comin применяет `main`;
  в `current-system` комin-сервисы, `auto-optimise-store`, `nix-gc.timer`;
  root эфемерный).

Следующий фокус (непосредственно после выполненных вертикалей):

- [F10](#f10-disposable-worker) — упаковка execution backend (r1s `r1sd`/containerd) закрыта в
  [f10-01](./roadmap/f10-disposable-worker/f10-01-package-r1s.md); впереди общий immutable Pi
  image и контейнерный Pi runtime (f10-04), worker credentials (f10-05) и acceptance
  (f10-06). Что именно Lattice фиксирует поверх r1s (task spec, lifecycle) решается по ходу.
- [F11](#f11-controller) — ждёт стабилизации task specification и ручного worker
  lifecycle в F10.

Отложены на потом:

- [F4](#f4-полезная-нагрузка) — остатки: drill без GitHub, bootstrap новой ноды, f4-05.
- [F9](#f9-caches-и-artifacts) — artifacts/S3.

Ещё не начатые вертикали (в порядке подхода):

- [F5](#f5-внешние-узлы), [F6](#f6-радио-и-mesh) — политика доверия внешних узлов и
  радиоканал.

До тех пор, пока не начат цикл F10→F11, эти вертикали остаются
в запланированных, а не в выполненных.
