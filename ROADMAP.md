# Роадмап Lattice

Верхнеуровневый план и детали живут в разных файлах, чтобы читать можно было с нужной глубины, не
погружаясь глубоко в проект. Каждая фича — отдельная вертикаль со своим каталогом
`roadmap/<feature-id>-<feature-slug>/`, где лежат файл фичи (`README.md`) и её задачи. Шаблоны для
заведения новых features и tasks лежат в [TEMPLATE_FEATURE.md](./roadmap/TEMPLATE_FEATURE.md) и
[TEMPLATE_TASK.md](./roadmap/TEMPLATE_TASK.md).

Номера фич — стабильные идентификаторы, а не требование выполнять все строки сверху вниз.
Открытые решения и отложенное — в [BACKLOG.md](./roadmap/BACKLOG.md).

## [F1. Одна железная нода](./roadmap/f1-one-node/README.md)

Проект перестаёт быть только проектом.

- **Статус:** 🚧 в работе
- **Готово, когда:** нода ставится с нуля по документации, переживает перезагрузку и сама применяет
  коммит из `main`.
- **Зависит от:** —

## [F2. Секреты и идентичность](./roadmap/f2-secrets-identity/README.md)

Безопасность на ключах, а не на закрытости.

- **Статус:** ✅ выполнена
- **Готово, когда:** репозиторий можно опубликовать целиком — доступа к узлам он не даёт.
- **Зависит от:** [F1](#f1-одна-железная-нода)

## [F3. Reticulum поверх TCP/IP](./roadmap/f3-reticulum-tcp/README.md)

Узлы связываются по интернету, не только в LAN.

- **Статус:** ✅ выполнена
- **Готово, когда:** с ноутбука открывается shell на узле за NAT через rnsh, связь переживает смену
  IP.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

## [F4. Полезная нагрузка](./roadmap/f4-payload/README.md)

Нода становится self-hosted средой и source origin.

- **Статус:** 🚧 в работе
- **Готово, когда:** конфиг распространяется без GitHub; на ноде работает прикладной сервис.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

## [F5. Внешние узлы](./roadmap/f5-external-nodes/README.md)

Чистая граница узла и политика доверия.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** узел из внешнего репозитория участвует в сети без доступа к чужим секретам.
- **Зависит от:** [F2](#f2-секреты-и-идентичность), [F3](#f3-reticulum-поверх-tcpip),
  [F4](#f4-полезная-нагрузка)

## [F6. Радио и mesh](./roadmap/f6-radio-mesh/README.md)

Работа на узком канале.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** два узла обмениваются данными по радио при отключённом интернете.
- **Зависит от:** [F3](#f3-reticulum-поверх-tcpip)

## [F7. LLM gateway](./roadmap/f7-llm-gateway/README.md)

Единая точка доступа к моделям и provider credentials.

- **Статус:** ✅ выполнена (включая f7-13 provider balancing, live-прогон 2026-09-14).
  Дальнейшая наблюдаемость (метрики `/metrics`, structured события, Grafana) вынесена в
  [F12](#f12-observability-метрики-gateway-графана), а не в follow-up закрытой F7.
- **Готово, когда:** клиенты используют только логические классы моделей, а отказ upstream
  обрабатывается заданной политикой.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

## [F8. Интерактивный Pi](./roadmap/f8-pi-runtime/README.md)

Основной harness работает непосредственно на ноде.

- **Статус:** ✅ выполнена
- **Готово, когда:** Pi TUI выполняет реальную задачу через gateway и воспроизводимый набор tools.
- **Зависит от:** [F7](#f7-llm-gateway)

## [F9. Caches и artifacts](./roadmap/f9-cache-artifact-plane/README.md)

Ускорение отделено от ценного результата.

- **Статус:** 🚧 в работе — caches-часть (f9-01..f9-03) выполнена и
  live-подтверждена; artifacts/S3 остаётся.
- **Готово, когда:** caches можно удалить без потери корректности, artifacts сохраняются отдельно.
- **Зависит от:** [F4-01](./roadmap/f4-payload/f4-01-radicle-seed-comin.md), [F7](#f7-llm-gateway),
  [F8](#f8-интерактивный-pi)

> 🔴 **Инцидент 2026-09-14:** `MemoryDenyWriteExecute=true` в песочнице Verdaccio
> ронял Node 24/V8 на ноде, каждый `comin`-switch падал (status 4) и нода
> переставала самообновляться из `main`. Исправлено: `MemoryDenyWriteExecute = false`
> для Verdaccio (как у llm-gateway/Bifrost). Полный контекст — в
> [f9-README](./roadmap/f9-cache-artifact-plane/README.md). Live-прогон 2026-09-14
> дополнительно закрыл все критерии f9-03 (см. [f9-03](./roadmap/f9-cache-artifact-plane/f9-03-verdaccio.md)).

## [F10. Disposable worker](./roadmap/f10-disposable-worker/README.md)

Задача выполняется в одноразовом окружении. Execution backend — готовый
[r1s](https://github.com/mytecor/r1s) (`r1sd`-allocator над `containerd`); временный `LocalExecutor`
из плана убран.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** полный цикл завершается результатом после уничтожения worker.
- **Зависит от:** [F8](#f8-интерактивный-pi), [F9](#f9-caches-и-artifacts)

## [F11. Controller](./roadmap/f11-controller/README.md)

Автоматизация очереди и жизненного цикла workers.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** задача переживает сбой controller/worker без потери или двойной публикации
  результата.
- **Зависит от:** [F10](#f10-disposable-worker)

## [F12. Observability (метрики gateway + Grafana)](./roadmap/f12-observability/README.md)

Метрики, структурированные события и дашборды для декларативной настройки LLM gateway.

- **Статус:** 🚧 в работе — [f12-01](./roadmap/f12-observability/f12-01-gateway-metrics-endpoint.md)
  реализован 2026-09-15 (`/metrics` + отдельный loopback-листенер).
- **Готово, когда:** числовые метрики (`/metrics` → Prometheus) и JSON-события (stdout → Alloy /
  Loki) видны в Grafana; конкретный запрос связывается по `request_id` до переходов в
  retry/fallback/race; димензии низкой cardinality, без публичных сервисов.
- **Зависит от:** [F7](#f7-llm-gateway)
- **Задачи:** [f12-01](./roadmap/f12-observability/f12-01-gateway-metrics-endpoint.md),
  [f12-02](./roadmap/f12-observability/f12-02-gateway-structured-events.md),
  [f12-03](./roadmap/f12-observability/f12-03-observability-stack.md),
  [f12-04](./roadmap/f12-observability/f12-04-grafana-dashboards.md)
