# Роадмап Lattice

Это указатель на роадмап, разложенный по уровням. Верхнеуровневый план и детали живут в разных
файлах, чтобы читать можно было с нужной глубины, не таща весь контекст сразу.

## Три уровня

| Уровень | Файл | Для кого | Что внутри |
| ------- | ---- | -------- | ---------- |
| **1. План** | [VISION.md](./VISION.md) | кто хочет понять, куда идём | цель, масштаб, транспорт, вехи, инварианты — без технических деталей |
| **2. Фичи** | [`features/`](features/README.md) | кто планирует работу | по файлу на фичу F1–F11 с задачами, зависимостями и критериями готовности |
| **3. Задачи** | [`tasks/`](features/README.md) | кто делает | отдельный файл на задачу + шаблон |

## Как пользоваться

- **«Что вообще происходит?»** — прочитайте `VISION.md`.
- **«Что за работа ближе всего?»** — [`features/`](features/README.md) (фичи по зависимостям) и реестр задач.
- **«За что берусь?»** — откройте конкретный файл в `tasks/`.
- **«Завожу новую задачу»** — скопируйте [TEMPLATE.md](features/TEMPLATE.md).

Два верхних уровня связаны напрямую: каждая веха в `VISION.md` ссылается на свою фичу в
`features/`, а каждая фича — обратно на веху. Номера фич — стабильные идентификаторы, а порядок
работы задаётся зависимостями и основным маршрутом в `VISION.md`.

## Связанные документы

- [ARCHITECTURE.md](../../ARCHITECTURE.md) — как устроен код (не роадмап).
- [BACKLOG.md](./BACKLOG.md) — открытые решения и отложенное.
- [DEPLOYMENT.md](../../DEPLOYMENT.md) — разворачивание нод.
- [CONTRIBUTING.md](../../CONTRIBUTING.md) — добавление новых узлов.

## Статус

Первая физическая нода развёрнута; задачи **F2 — секреты и идентичность узла** и
**F3 — Reticulum поверх TCP/IP** выполнены. На homelab включён авторизованный rnsh через
публичные peers Sydney/ReticulumNet. После перевода Mac на мобильный hotspot доступ с прежними
identity и destination восстановился без общей LAN и без перезапуска сервисов. Создание второй
постоянной NixOS-ноды отменено в [f3-03](f3-reticulum-tcp/f3-03-second-node-rnsh.md). В F4 на homelab
развёрнут selective Radicle seed, `comin` читает его локальное storage первым remote, а публичная
реплика проверена чистым клиентом. Caddy публикует первый прикладной node-status endpoint и
Radicle HTTP API; f4-02 выполнена. Для f4-01 остаются строгий drill без GitHub и полный bootstrap
новой NixOS-ноды. Spike f7-01, модуль f7-02 и контракт logical models f7-03 дали временный
`token_proxy` runtime на homelab. Реальная Gonka-интеграция выявила ограничения динамического
catalog/scoped routing, а f7-06 отклонила Go LIP из-за обязательного функционального fork.
Архитектура теперь — собственный Go proxy поверх Bifrost Go API: f7-07 выполнил cutover, а f7-08
удалил legacy `token_proxy` из активной конфигурации. Базовая F7 закрыта 2026-09-07: обязательные
режимы routing (retry, cooldown, fallback, priority, race, hedge, streaming, cancellation), health
surface и structured diagnostics подтверждены Go-тестами и evaluation checks. Live acceptance
выявила мультипликативный fanout полного race и hedged retries; bounded provider routing вынесен в
[f7-09](./f7-llm-gateway/f7-09-bounded-provider-routing.md). Запускается **F8 — интерактивный Pi
runtime**; f8-01 завершена: Pi закреплён и устанавливается через pnpm без
пользовательской ручной установки. В f8-02 выполнена декларативная привязка Pi к gateway:
`lattice.pi` генерирует store JSON и материализует `~/.pi/agent/{settings,models}.json` симлинками,
`discoverModels = false`, только логические классы `standard`/`stupid` по loopback, с NixOS-проверкой
`tests/pi-config.nix`. Client credential закрыт как не требующийся (client auth в gateway выключен,
соединение loopback-only, порт `9208` един в `profiles/networking/ports.nix`); интерактивная проверка
streaming остаётся за f8-04 (TUI). В f8-03 выполнен воспроизводимый tool profile: единый базовый
контракт `bash/git/tools` в `profiles/pi/base-tools.nix` (нода и devShell), расширение проекта через
`lattice.pi.tools` / `pkgs.mkShell { inputsFrom = [ pkgs.lattice.pi-develop-shell ]; }` без изменения
рантайма, контракт окружения (PATH, locale, git identity boundary) в `/etc/pi.env`, и smoke check
`tests/pi-tool-profile.nix`, проходящий на x86_64-linux. В f8-06 закрыт надёжный сетевой путь:
постоянный multi-session ACP daemon (`hydra-acp` + `pi-acp`) поверх одного LAN-эндпоинта
`ws://acp.<nodename>.local/`, с трансформером `acp-normalizer` для стабильного `messageId` и
acceptance-тестами параллельных сессий/клиентов. f8-04 и f8-05 закрыты по решению: Pi работает, а
локальный Pi TUI не используется — вся интерактивная работа идёт через ACP (Ferngeist), отдельного
Pi-native RPC-контракта нет (роль RPC entry point для F10 выполняет ACP endpoint из f8-06). Все
задачи F8 закрыты; следующая фича — F9 (cache/artifact plane).
Основной маршрут дальше:
Pi → cache/artifact plane → disposable worker → controller.
Незакрытые вопросы отслеживаются в
[BACKLOG.md](./BACKLOG.md); работа идёт рывками, поэтому `main` всегда собирается.
