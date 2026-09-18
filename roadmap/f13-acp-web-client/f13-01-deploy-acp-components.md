# f13-01. Развернуть web-клиент acp-components против LAN ACP endpoint

Фича: [F13 — Web-клиент ACP](./README.md). Зависит от
[f8-06](../f8-pi-runtime/f8-06-network-acp-daemon.md) — endpoint `ws://acp.<nodename>.local/`,
daemon `hydra-acp` и закреплённый `pi-acp` уже существуют.

## Контекст

Ferngeist — единственный подтверждённый клиент ACP ingress из [f8-06](../f8-pi-runtime/f8-06-network-acp-daemon.md).
Готовый open-source workbench
[`zvzuola/acp-components`](https://github.com/zvzuola/acp-components) (React + framework-agnostic
core, лицензия MIT) закрывает UI-часть: мульти-агент, мульти-воркспейсы, параллельные сессии в
split-панелях, tool calls, permissions, стриминг. Его транспорт —
`WebSocketTransport` (`packages/core/src/transport/ws.ts`): сырой WebSocket JSON-RPC к ACP-агенту.
Задача — развернуть его декларативно против существующего endpoint и зафиксировать проверенный
результат совместимости, не меняя daemon.

## Что сделать

- [ ] 1. **Закрепить источник и версии.** Зафиксировать upstream-коммит `acp-components`
      (пакеты `@acp-components/core` / `@acp-components/react`) по образцу
      [`packages/hydra-acp`](../../packages/hydra-acp/README.md): источник, лицензия, процедура
      обновления; для сборки — pnpm lock + hashes по образцу
      [`pnpm-cli-builder`](../../packages/pnpm-cli-builder/README.md).
- [ ] 2. **Проверить форму соединения.** Клиентский `WebSocketTransport` не выставляет subprotocol,
      а Lattice ingress закреплён как «чистый ACP WebSocket с subprotocol `acp.v1`»
      ([tests/acp-ingress-smoke.mjs](../../tests/acp-ingress-smoke.mjs),
      [ARCHITECTURE.md](../../ARCHITECTURE.md)). Проверить фактически: принимает ли
      закреплённый `hydra-acp 0.1.183` соединение без `Sec-WebSocket-Protocol`. Если нет —
      минимальный Lattice-owned адаптер (обёртка/патч транспорта) остаётся внутри пакета клиента и
      не меняет daemon; отрицательный результат зафиксировать.
- [ ] 3. **Собрать прод-бандл клиента** (демо из `examples/demo` как база: Vite build,
      `createWebPlatform`, websocket-агент) и раздавать его декларативно с ноды через Caddy —
      по образцу статических сайтов
      [`profiles/app-services`](../../profiles/app-services/README.md) (LAN-only host вида
      `acp-ui.<nodename>.local`, alias через avahi/mdns publisher).
- [ ] 4. **Конфигурация по умолчанию**: в бандле один преднастроенный агент
      `transport: { type: 'websocket', url: 'ws://acp.<nodename>.local/' }`, чтобы клиент
      подключался к существующему endpoint без ручного ввода; пользовательские агенты — через
      built-in persistence клиента.
- [ ] 5. **Acceptance-проверка** (по контракту из f8-06): подключение к
      `ws://acp.<nodename>.local/`, создание ≥2 параллельных сессий, reconnect c
      `session/list` + `session/attach`, два клиента одной live-сессии; поведение стриминга с
      включённым глобально [`acp-normalizer`](../../packages/acp-normalizer/README.md) (клиент
      ключует чанки по `messageId` — проверить, что нормализованные стабильные id рендерятся как
      одно сообщение). Результат зафиксировать в этой задаче.
- [ ] 6. **Контракт-тест**: сборка/оценка конфигурации (`nix flake check`); клиентский ingress
      не публикует daemon напрямую, токен Hydra не утекает в клиентский бандл/конфиг
      (граница trusted LAN из f8-06 сохраняется).

## Критерий готовности (Definition of Done)

- [ ] Клиент доступен в LAN по фиксированному имени, развёрнут декларативно из закреплённого
      источника, и не требует изменений в конфигурации daemon или Caddy ingress самого ACP
      endpoint.
- [ ] Через клиента воспроизведён acceptance из [f8-06](../f8-pi-runtime/f8-06-network-acp-daemon.md):
      параллельные сессии, reconnect с восстановлением истории, два клиента на одной live-сессии;
      зафиксировано поведение стриминга/permissions. Если совместимость не подтвердилась —
      воспроизводимый отрицательный результат и объём минимального shim зафиксированы здесь, а
      не молчаливая подмена endpoint.

## Затрагиваемые файлы / слои

- `packages/acp-web/` (новый) — закреплённый source/build клиента.
- [`profiles/app-services/`](../../profiles/app-services/README.md) — LAN Caddy site и mdns alias.
- [`profiles/tcp-gateway/`](../../profiles/tcp-gateway/README.md) — только если потребуется
  общий шаблон alias; сам ACP ingress не меняется.
- [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/README.md) — включение сервиса
  и `/persist` для пользовательского состояния, если понадобится.
- [`tests/`](../../tests/README.md) — контракт-тест.
- [`ROADMAP.md`](../../ROADMAP.md) — веха F13.

## Открытые вопросы

- Принимает ли `hydra-acp` WebSocket-соединения без subprotocol `acp.v1` (шаг 2 решает до
  основной сборки). Если нет — объём минимального shim фиксируется в задаче.
- Хостинг статики из flake (plain Caddy `root`/`file_server` vs derivation-пакет) — выбрать при
  реализации по образцу существующих LAN-сервисов.
