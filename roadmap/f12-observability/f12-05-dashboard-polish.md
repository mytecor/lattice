# f12-05. Доработка Grafana-дашбордов и багфиксы наблюдаемости gateway

Фича: [F12 — Observability](./README.md). Follow-up к [f12-04](./f12-04-grafana-dashboards.md)
(дашборды) — улучшает панели и чинит два бага наблюдаемости: утечку счётчика
`llm_requests_in_flight` и неверный фильтр `request_id` (regex вместо равенства).

## Контекст

Дашборды f12-04 уже покрывают числовые метрики и расследование по `request_id`. Ревью выявило
точечные изъяны, которые мешают читать дашборд с первого взгляда (цель F12): переменная `status`
заведена в templating, но не используется ни в одном `expr`; y TTFT только p95 (нет p50/p99);
нет видимого распределения и p99 у latency; панель-логи по `request_id` не связана data links с
дашбордом расследования; `llm_gateway_build_info` (версия сборки) не показан; панель «Errors»
считает успешные attempts (`error_type=""`) как ошибки; duplicate panel `id`s между дашбордами
мешают копированию панелей в UI. Позднее на живом дашборде оказалось, что `llm_requests_in_flight`
растёт медленно, но монотонно (≈200 за 6 часов): это оказалось не ростом нагрузки
(fanout attempts/requests ≈ 1.2, попыток единицы), а **утечкой декремента** (см. п.8), и
«Счёт по event для request_id» путал события всех запросов из-за regex-фильтра `=~""` (см. п.9).

## Что сделать

- [x] 1. **Использовать переменную `status`**: RPS по route разделить по `status` (через
      `status=~"$status"`) — переменная перестанет быть мёртвой; `llm_attempts_total`
      в «Errors» фильтровать `error_type=~".+"`, чтобы успешные attempt-ы не звонили как ошибки.
- [x] 2. **TTFT: добавить p50 и p99** к существующему p95 (`histogram_quantile` на
      `llm_ttft_seconds_bucket`), `legendFormat "p99 {{model}}"`.
- [x] 3. **Latency: добавить p99 и топ-1% границу**: p95/p99 + верхний бакет через `_count`
      (`histogram_quantile(0.99, rate(llm_request_duration_seconds_bucket …))`), чтобы был виден
      хвост, а не только p95.
- [x] 4. **Data links из логов в расследование**: в `llm-gateway` панель «Связка по
      `request_id`» — data link по значению `request_id` на дашборд «Loki / Расследование»
      (`var-request_id=${__value.raw}`). (В `loki-investigation` self-link не нужен: дашборд уже
      показывает именно этот `request_id`.)
- [x] 5. **Версия сборки в runtime**: stat-панель с `llm_gateway_build_info{service="llm-gateway"}`
      (значение + имя серии = версия), чтобы деплоенная версия бинаря была видна с первого взгляда.
- [x] 6. **Panel `id`s**: проверено — в каждом дашборде id уникальны в пределах файла (дубли
      только *между* дашбордами, что Grafana не волнует — id не обязан быть глобально уникальным,
      только уникален в рамках одного дашборда). Правок не потребовалось.
- [x] 7. **Обновить контракт-тест** `tests/grafana-dashboards.nix`: добавить проверки на наличие
      `status=~"$status"`, `error_type=~".+"`, p99 (0.99), data links на loki-investigation,
      `llm_gateway_build_info`, и `request_id="${request_id}"` (не `=~`).
- [x] 8. **Починить утечку `llm_requests_in_flight` (баг кода gateway)**: декремент жил только
      в main loop `raceRoute` (при чтении из `sc.results`); при победе одной ветки loop выходит
      и канал не читается → проигравшие ветки навсегда +1 в gauge. Фикс: `defer DecrInFlight`
      в самой `runBranch` (срабатывает на всех путях выхода), убрать дублирующий декремент из
      `observeBranch`/`observeBranchCancelled`. Регрессия
      `TestRaceReleasesInFlightGauge` (падает без фикса, зелёный с фиксом).
- [x] 9. **Починить «Счёт по event для request_id»**: `request_id=~"${request_id}"` при пустой
      переменной становился `~""` и матчил ВСЕ запросы (панель показывала весь stream).
      Заменить в трёх панелях (llm-gateway «Связка», loki-investigation «Счёт» и «Все события»)
      на точное `request_id="${request_id}"` — пустой выбирающий → 0 серий (ничего не показывать).

## Критерий готовности (Definition of Done)

- [x] 1. Дашборд «LLM Gateway» читается с первого взгляда: статус (success/failed) виден по RPS,
      успешные attempt-ы не считаются ошибками, latency/TTFT показывают p50/p99 и верхнюю
      границу, лог-панель по `request_id` ведёт в «Loki / Расследование».
- [x] 2. Дашборд «Gateway runtime» показывает версию сборки (`llm_gateway_build_info`).
- [x] 3. Контракт-тест `tests/grafana-dashboards.nix` дополнен и проходит: все assert-ы
      срабатывают при eval (`nix eval .#checks.x86_64-linux.grafana-dashboards.name` прошёл);
      полный `nix flake check --no-build` на `x86_64-linux` (локально на darwin сборка
      x86_64-linux runCommand недоступна — eval и есть проверка контракта). Live-проверка
      на ноде — шаг деплоя (как в f12-03/f12-04).
- [x] 4. `llm_requests_in_flight` больше не растёт монотонно: регрессионный тест
      `TestRaceReleasesInFlightGauge` покрывает победу + проигравших (все тесты пакета зелёные,
      `go vet` чистый).
- [x] 5. «Счёт по event для request_id» с пустой переменной показывает 0 серий (не весь stream),
      с введённым `request_id` — только события этого запроса.

## Затрагиваемые файлы / слои

- `modules/grafana/dashboards/llm-gateway.json` — status-фильтр RPS, p50/p99 TTFT/latency,
  data link в расследование, `error_type=~".+"` в Errors, `sum by (provider)` в In-flight,
  `request_id="${request_id}"` (точное равенство).
- `modules/grafana/dashboards/gateway-runtime.json` — stat «Версия сборки» (`llm_gateway_build_info`).
- `modules/grafana/dashboards/loki-investigation.json` — `request_id="${request_id}"` в двух панелях
  («Счёт по event», «Все события запроса») вместо `=~`.
- `packages/llm-gateway/scheduler.go` — `defer DecrInFlight` в `runBranch` (багфикс утечки).
- `packages/llm-gateway/router.go` — убраны дублирующие `DecrInFlight` из `observeBranch*`.
- `packages/llm-gateway/router_test.go` — регрессия `TestRaceReleasesInFlightGauge`.
- `tests/grafana-dashboards.nix` — новые контракт-ассерты (status, p50/p99, data link,
  `llm_gateway_build_info`, `request_id=` vs `=~`).
- Документация: `modules/grafana/dashboards/README.md`, `modules/grafana/README.md`,
  `roadmap/f12-observability/README.md`, `ROADMAP.md`.

## Реализация 2026-09-16

- `llm-gateway.json`: RPS теперь `by (route, status)` + `status=~"$status"` (переменная `status`
  перестала быть мёртвой); «Errors» фильтрует `error_type=~".+"` (успешные attempt-ы
  с `error_type=""` больше не звонят как ошибки); TTFT и Latency показывают p50/p95/p99
  (`histogram_quantile(0.50/0.95/0.99)`); In-flight свёрнут `sum by (provider)`;
  лог-панель «Связка по `request_id`» получила data link на «Loki / Расследование»
  (`/d/loki-investigation?var-request_id=${__value.raw}&${__url_time_range}`).
- `gateway-runtime.json`: stat «Версия сборки gateway» — `llm_gateway_build_info{service="llm-gateway"}`,
  legend `{{version}}`, `textMode value_and_name` — видна деплоенная версия бинаря.
- **Багфикс утечки `llm_requests_in_flight`**: медитация на живой ноде показала медленный
  монотонный рост (~200/6ч) при fanout ≈1.2 и единичных попытках; корень — `DecrInFlight` жил
  только в main loop, который выходит при победе, бросая канал с результатами проигравших.
  Перенёс декремент в `runBranch` через `defer`, убрал дубликаты из `observeBranch*`;
  регрессия `TestRaceReleasesInFlightGauge` падает без фикса (проверено), зелёный с фиксом.
- **Багфикс «Счёт по event»**: `request_id=~"${request_id}"` при пустой переменной → `~""`
  матчил все запросы. Заменил на `request_id="${request_id}"` в трёх панелях; контракт-тест
  теперь требует `=` и отвергает `=~`.
- Panel `id`s: выяснено, что в каждом дашборде id уже уникальны в пределах файла (пересечение
  только между дашбордами — это допустимо, Grafana требует уникальность в рамках одного
  дашборда); отдельных правок не потребовалось.
- Контракт-тест дополнен ассертами (status, p50/p99, data link, build_info, request_id=); eval
  прошёл (`nix eval .#checks.x86_64-linux.grafana-dashboards.name` → `"grafana-dashboards-contract"`).
- `go vet` чистый, все тесты пакета llm-gateway зелёные (`go test ./...` → ok).
