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

### [F10. Agent runtime](./roadmap/f10-disposable-worker/README.md)

> Disposable OCI container предоставляет long-lived ACP endpoint внутри конкретного r1s run.

- **Статус:** 🟡 начата — [f10-01](./roadmap/f10-disposable-worker/f10-01-package-r1s.md) (упаковка
  execution backend r1s/r1sd) закрыта 2026-09-16; [f10-02](./roadmap/f10-disposable-worker/f10-02-deploy-r1sd.md)
  (разворачивание `r1sd`-allocator на ноде) закрыта 2026-10-03 — `worker-runtime` активен на ноде,
  allocator готов в mesh, `r1s`-клиент с ноды доходит до него. Следующим — OCI agent image с
  `git`, Pi, `pi-acp`, workspace bootstrap и ACP listener (f10-04), затем credentials (f10-05) и
  disposability acceptance (f10-06).
- **Готово, когда:** r1s запускает одноразовый agent container, а ACP client подключается к его
  long-lived endpoint; уничтожение контейнера не теряет уникальное durable state.
- **Зависит от:** [F8](#f8-интерактивный-pi), [F9](#f9-caches-и-artifacts)
  (только caches-часть; artifacts/S3 отложена)

### [F11. Git task pipeline](./roadmap/f11-git-task-pipeline/README.md)

> Git revisions превращаются в `TaskSpec`, а terminal `TaskResult` возвращается в Git.

- **Статус:** ⏳ ещё не начата — архитектура и `task.md` contract зафиксированы в
  [TASK_EXECUTION.md](./TASK_EXECUTION.md); реализация начинается с generic `git-watchd`, затем
  `taskd` и deterministic dummy workload.
- **Готово, когда:** task revision без terminal result обнаруживается и исполняется после restart
  `git-watchd`, `taskd` или `agentd`, а terminal result фиксируется в Git.
- **Зависит от:** [F10](#f10-agent-runtime)

### [F19. Agent execution loop](./roadmap/f19-agent-execution-loop/README.md)

> `agentd` выполняет `TaskSpec` через r1s, ACP tunnel и bounded verification loop.

- **Статус:** ⏳ ещё не начата — после доказанного dummy pipeline F11.
- **Готово, когда:** disposable agent меняет source repository, проверки проходят или возвращают
  ACP feedback, а `agentd` выдаёт terminal `TaskResult` и восстанавливается без durable local state.
- **Зависит от:** [F10](#f10-agent-runtime), [F11](#f11-git-task-pipeline)

### [F20. Planning loop](./roadmap/f20-planning-loop/README.md)

> Planning agent превращает разговор в Git commit с `task.md` через существующий ACP ingress.

- **Статус:** ⏳ ещё не начата — после single-agent execution loop F19.
- **Готово, когда:** разговор приводит к новой task revision, а весь путь до terminal result
  запускается без прямого вызова `agentd` или r1s planning agent'ом.
- **Зависит от:** [F11](#f11-git-task-pipeline), [F19](#f19-agent-execution-loop)

### [F21. Multi-agent](./roadmap/f21-multi-agent/README.md)

> Root agent запускает ephemeral child workloads через r1s и общается с ними по ACP tunnels.

- **Статус:** ⏳ отложена до завершения single-agent pipeline.
- **Готово, когда:** nested agents работают без durable Lattice control state; переживающая root
  работа оформляется отдельной `task.md`.
- **Зависит от:** [F19](#f19-agent-execution-loop)

### [F4. Полезная нагрузка](./roadmap/f4-payload/README.md)

> Нода становится self-hosted средой и source origin.

- **Статус:** 🟡 частично — source bootstrap (f4-01) и app services (f4-02, f4-03, f4-04)
  работают на homelab; drill без GitHub, полный bootstrap новой ноды и f4-05
  (Yggdrasil-ingress) отложены на потом.
- **Готово, когда:** конфиг распространяется без GitHub; на ноде работает прикладной сервис.
- **Не блокирует:** [F10](#f10-agent-runtime), [F11](#f11-git-task-pipeline) — зависят только
  от f4-01 (Radicle seed/comin), который выполнен.
- **Зависит от:** [F1](#f1-одна-железная-нода), [F2](#f2-секреты-и-идентичность)

### [F9. Caches и artifacts](./roadmap/f9-cache-artifact-plane/README.md)

> Ускорение отделено от ценного результата.

- **Статус:** 🟡 частично — caches-часть (f9-01..f9-03) выполнена и live-подтверждена 2026-09-14;
  artifacts/S3 намеренно отложена на сильно потом (не блокирует F10/F11).
- **Готово, когда:** caches можно удалить без потери корректности, artifacts сохраняются отдельно.
- **Не блокирует:** [F10](#f10-agent-runtime), [F11](#f11-git-task-pipeline).
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
- **Не блокирует:** [F10](#f10-agent-runtime), [F11](#f11-git-task-pipeline).

### [F6. Радио и mesh](./roadmap/f6-radio-mesh/README.md)

> Работа на узком канале.

- **Статус:** ⏳ ещё не начата
- **Готово, когда:** два узла обмениваются данными по радио при отключённом интернете.
- **Зависит от:** [F3](#f3-reticulum-поверх-tcpip)

### [F14. SSO через Authentik](./roadmap/f14-sso-authentik/README.md)

> Центральный single sign-on для пользовательских web-сервисов ноды; единый вход через Authentik,
> стоящий за существующим Caddy.

- **Статус:** ⏳ ещё не начата — [f14-01](./roadmap/f14-sso-authentik/f14-01-deploy-authentik-sso.md)
  (развернуть Authentik нативно через NixOS и встроить как центральный SSO: собственный модуль
  поверх `pkgs.authentik`, agenix-секреты, Caddy ForwardAuth для сервисов без собственного SSO,
  нативный OIDC для Grafana, защита только web-UI API-сервисов, разделение «люди» и
  машина-к-машине).
- **Готово, когда:** один вход в Authentik открывает защищённый веб-UI всех подключённых сервисов;
  сервисы без собственного SSO защищены Caddy ForwardAuth; Grafana входит через OIDC; M2M-пути не
  зависят от браузерной сессии; у API-сервиса с раздельными UI/API защищён только UI.
- **Зависит от:** [F4](#f4-полезная-нагрузка), [F2](#f2-секреты-и-идентичность)
- **Не блокирует:** [F10](#f10-agent-runtime), [F11](#f11-git-task-pipeline).

### [F16. Context transformation](./roadmap/f16-context-transformation/README.md)

> Встроенное управление контекстом LLM gateway: compression старых tool outputs,
> reusable summaries и query-dependent selection при сохранении полной исходной истории.

- **Статус:** ⏳ запланирована 2026-09-21, ещё не начата; задачи
  [f16-01..f16-09](./roadmap/f16-context-transformation/README.md#задачи-и-порядок).
- **Готово, когда:** provider-facing context укладывается в budget, recent tail сохраняется
  verbatim, неизменившиеся segments переиспользуют cache; пройдена runtime-приёмка MVP.
- **Зависит от:** [F7](#f7-llm-gateway); наблюдаемость использует
  [F12](#f12-observability-метрики-gateway--grafana).
- **После MVP:** embeddings/hybrid retrieval, hierarchical summaries и raw-page promotion.

### [F17. Аварийный доступ и хотспот](./roadmap/f17-wifi-hotspot-switch/README.md)

> Переключение Wi-Fi-радио: проводной аплинк есть → точка доступа, нет → клиент.

- **Статус:** 🟢 завершена 2026-09-29; задачи
  [f17-01..f17-04](./roadmap/f17-wifi-hotspot-switch/README.md#порядок-работ).
- **Готово, когда:** на многодомной ноде вставленный провод переводит Wi-Fi в точку доступа
  (hostapd/dnsmasq/NAT), выдёргивание возвращает в клиент; в любой момент активно ровно одно
  состояние (STA **или** AP), переключение переживает reboot и гонки кабеля.
- **Зависит от:** [F1](#f1-одна-железная-нода); базируется на модели многодомной ноды из
  [F4](#f4-полезная-нагрузка) (`tcp-gateway`) и зафиксированном результате про одновременный
  STA+AP (снятый `wireless-hotspot`).

### [F18. Браузерный стек для агентов](./roadmap/f18-browser-agent-stack/README.md)

> Отдельный браузерный runtime для агентов: Jev → browser-harness → Foxbridge → Camoufox.
> Оригинальный `jev-ultrafast` без форка, все Firefox-совместимости в Foxbridge.

- **Статус:** 🟢 завершена 2026-09-23 (приёмка [f18-12](./roadmap/f18-browser-agent-stack/f18-12-acceptance.md));
  11/12 пунктов приняты фактом на живой ноде, п.7 (реальная web-задача Jev) снят с блокировки —
  homelab LLM-gateway отвечает на chat completions, остаётся один прогон f18-11.
- **Готово, когда:** оригинальный `jev-ultrafast` запускается с `BU_CDP_URL` без локальных
  изменений, браузером фактически является Camoufox, между ними работает Foxbridge, Jev
  закрывает минимум одну полноценную web-задачу, fingerprint Camoufox сохраняется, стек
  стартует декларативно на NixOS через systemd, CDP недоступен извне хоста, есть
  smoke/integration test полного пути.
- **Зависит от:** [F4](#f4-полезная-нагрузка) (app services на ноде), [F2](#f2-секреты-и-идентичность)
  (секреты через agenix), частично от [F8](#f8-интерактивный-pi) и [F10](#f10-agent-runtime)
  для интерфейса `browser_task` для Pi.

## Выполненные (в порядке закрытия)

### [F2. Секреты и идентичность](./roadmap/f2-secrets-identity/README.md)

> Безопасность на ключах, а не на закрытости.

- **Статус:** ✅ выполнена 2026-09-04
- **Готово, когда:** репозиторий можно опубликовать целиком — доступа к узлам он не даёт.
- **Зависит от:** [F1](#f1-одна-железная-нода)

### [F3. Reticulum поверх TCP/IP](./roadmap/f3-reticulum-tcp/README.md)

> Узлы связываются по интернету, не только в LAN.

- **Статус:** ✅ основной критерий выполнен 2026-09-05; follow-up
  [f3-05](./roadmap/f3-reticulum-tcp/f3-05-reticulum-go-daemon.md) на перевод shared daemon с
  patched `rns-rs` на Reticulum-Go запланирован 2026-10-04
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
  follow-up f7-14 (декларативные модели + p2c) реализован 2026-09-16, отменён
  2026-09-28 (сахар `models`/`pipeline` убран, конфиг снова плоский).
  Дальнейшая наблюдаемость
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
  `request_id`) реализована 2026-09-16. Follow-up наблюдаемости ноды:
  [f12-06](./roadmap/f12-observability/f12-06-node-system-metrics.md) (Go `node-status`, system
  metrics, service stability, dashboard) реализован 2026-09-29 и ждёт live-приёмки;
  [f12-07..f12-09](./roadmap/f12-observability/README.md#follow-up-наблюдаемость-ноды)
  фиксируют alerts, storage health и synthetic/operational probes.
- **Готово, когда:** числовые метрики (`/metrics` → Prometheus) и JSON-события (stdout → Alloy /
  Loki) видны в Grafana; конкретный запрос связывается по `request_id` до переходов в
  retry/fallback/race; димензии низкой cardinality, без публичных сервисов.
- **Зависит от:** [F7](#f7-llm-gateway)

### [F15. Разработка с ноды (node dev-loop)](./roadmap/f15-node-dev-loop/README.md)

> Полный цикл работы над Lattice прямо с ноды через ACP: сессии открываются в рабочем checkout
> на ноде, публикация `main` — в Radicle и GitHub с самой ноды, deploy — штатным `comin`.

- **Статус:** ✅ выполнена 2026-09-24 —
  [f15-01](./roadmap/f15-node-dev-loop/f15-01-workspace-checkout.md) (рабочий checkout на ноде +
  server-side `defaultCwd`) реализован и проверен;
  [f15-02](./roadmap/f15-node-dev-loop/f15-02-publish-access.md) (push-доступы: rad-peer,
  RAD_HOME в env, deploy key + ssh-алиас `github-lattice`, делегирование Radicle revision d888fa4,
  threshold 1-of-2) закрыта;
  [f15-03](./roadmap/f15-node-dev-loop/f15-03-dev-loop-acceptance.md) (acceptance) закрыта
  2026-09-24 — из ACP-сессии на ноде doc-правка → commit → `git push publish main` доехала
  до Radicle и GitHub, comin применил; нативный `nix flake check --no-build` — `all checks passed!`.
- **Готово, когда:** из ACP-сессии на ноде коммит доезжает до Radicle и GitHub одним
  `git push publish main` и применяется нодой через comin; состояние переживает reboot.
- **Зависит от:** [F4](#f4-полезная-нагрузка) (f4-01 выполнен), [F8](#f8-интерактивный-pi)
