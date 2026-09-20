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

- [x] 1. **Закрепить источник и версии.** Зафиксировать upstream-коммит `acp-components`
      (пакеты `@acp-components/core` / `@acp-components/react`) по образцу
      [`packages/hydra-acp`](../../packages/hydra-acp/README.md): источник, лицензия, процедура
      обновления; для сборки — pnpm lock + hashes по образцу
      [`pnpm-cli-builder`](../../packages/pnpm-cli-builder/README.md).
- [x] 2. **Проверить форму соединения.** Клиентский `WebSocketTransport` не выставляет subprotocol,
      а Lattice ingress закреплён как «чистый ACP WebSocket с subprotocol `acp.v1`»
      ([tests/acp-ingress-smoke.mjs](../../tests/acp-ingress-smoke.mjs),
      [ARCHITECTURE.md](../../ARCHITECTURE.md)). Проверить фактически: принимает ли
      закреплённый `hydra-acp 0.1.183` соединение без `Sec-WebSocket-Protocol`. Если нет —
      минимальный Lattice-owned адаптер (обёртка/патч транспорта) остаётся внутри пакета клиента и
      не меняет daemon; отрицательный результат зафиксировать.
- [x] 3. **Собрать прод-бандл клиента** (демо из `examples/demo` как база: Vite build,
      `createWebPlatform`, websocket-агент) и раздавать его декларативно с ноды через Caddy —
      по образцу статических сайтов
      [`profiles/app-services`](../../profiles/app-services/README.md) (LAN-only host вида
      `acp-ui.<nodename>.local`, alias через avahi/mdns publisher).
- [x] 4. **Конфигурация по умолчанию**: в бандле один преднастроенный агент
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

- [x] Клиент доступен в LAN по фиксированному имени, развёрнут декларативно из закреплённого
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

## Шаг 2 выполнен: форма соединения подтверждена (2026-09-19)

Подключение проводилось с машины в той же LAN живыми WebSocket-запросами против
`ws://acp.mytecor-homelab.local/` (192.168.60.184), ровно в форме `WebSocketTransport`
`acp-components` (`packages/core/src/transport/ws.ts`): `new WebSocket(url)` **без** второго
аргумента subprotocol, затем дефолтные ACP JSON-RPC-сообщения.

Результат — **положительный, shim для формы соединения не требуется**:

- **Без subprotocol** (как acp-components): handshake проходит, `negotiated-protocol=""`
  (сервер не эхо возвращает subprotocol), и endpoint отвечает на `initialize`
  (`agentInfo { name: hydra, version: 0.1.183 }`, sessionCapabilities и т.д.).
- **С `['acp.v1']`** (классический клиент, форма Ferngeist) для сравнения: тоже открывается,
  `protocol="acp.v1"`. Обе формы стабильно открываются в повторных прогонах.

Поведение сервера совпадает с кодом daemon: `selectAcpSubprotocol`
(`src/daemon/ws-protocol.ts`) возвращает `false` (не отклоняя upgrade) для клиентов без `acp.v1`,
сохранение совместимости заявлено в комментарии. `WebSocketTransport` подключится к
существующему endpoint без изменений daemon или Caddy ingress; кодировать subprotocol в
Lattice-пакете клиента не нужно.

## Шаги 1, 3, 4 выполнены: источник закреплён, прод-бандл собран и провижен в store (2026-09-20)

**Источник закреплён (шаг 1).** Upstream-коммит `zvzuola/acp-components`
`1708c20274c9f15ee3a072009e5ca9fd3b71a9de` (fetchzip hash
`sha256-Jn/q4fAUjL+QikWOzj5VQPBD9Ch4r9obV2uDwzYKmt8=`) зафиксирован в `packages/acp-web/package.nix`;
workspace `pnpm-lock.yaml` закреплён через `fetchPnpmDeps` (fetcherVersion 4, hash
`sha256-TwS7s8OqWKfPcbMzqCuPtfY1GZPYtEHN3WewSc3+0kA=`). Процедура обновления — по образцу
[`packages/hydra-acp`](../../packages/hydra-acp/README.md) / [`packages/pnpm-cli-builder`](../../packages/pnpm-cli-builder/README.md).

**Сборка прод-бандла (шаг 3).** Полный workspace-билд (`pnpm build` core+react, затем Vite
`build` демо `examples/demo`) успешно собран на целевой ноде `mytecor-homelab`
(x86_64-linux) в Nix-песочнице. Особенность: штатный `pnpmConfigHook` в песочнице не
пересобирал SQLite-индекс v11-стора (pnpm считал offline-store пустым и уходил в сеть,
EAI_AGAIN) — в `configurePhase` процедура (извлечение `pnpm-store.tar.zst` + реконструкция
`v11/index.db` из `.sql`-дампа + arch/platform + store-dir) воспроизведена явно, и `pnpm
install --offline --ignore-scripts --frozen-lockfile` переиспользовал весь FOD-стор (0
сетевых обращений). Результат — store-path
`/nix/store/9jjxq8a6sk8bzb4pmnvywyf0x9ngymgh-acp-web-0.1.0-20260919` (13M, index.html + 95
ассетов), раздаётся Caddy `file_server` с SPA-fallback на `index.html`.

**Конфигурация по умолчанию (шаг 4).** В бандл вшит один преднастроенный агент через
`patch-main-ts.mjs` (чистая правка `examples/demo/src/main.tsx`, ломается loudly при изменении
upstream-формы): `VITE_ACP_ENDPOINT`-override или вывод endpoint из serving-имени
`acp-ui.<node>.local` → `ws://acp.<node>.local/`. В собранном бандле подтверждены строки
`"ACP Endpoint"`, `acp-ui.` и `ws://` — агент попадает в прод-минифицированный JS. Пользовательские
агенты — через built-in persistence клиента (не трогаем daemon/ingress).

## Деплой на ноду выполнен: acp-ui доступен в LAN (2026-09-20)

`main` опубликован в Radicle и GitHub (общий remote `publish`), нода `mytecor-homelab`
переключилась через comin (`switch successfully terminated`). Новые юниты `acp-ui-mdns.service`
и `node-status-mdns.service` активны; `caddy.service` перезагружен с новым конфигом.

Проверено с dev-машины в той же LAN:

- `acp-ui.mytecor-homelab.local` резолвится по mDNS → `192.168.60.184` (нода);
- `GET http://acp-ui.mytecor-homelab.local/` → **HTTP 200**, отдаётся `<title>acp-components
  interactive demo</title>` + ассет `index-CsUzZzjs.js` из store-path пакета acp-web
  (SPA-fallback: все пути кроме реальных файлов → `index.html`, клиентская маршрутизация);
- прод-бандл подтверждён (извлечён из store): runtime-деривация defaults-агента на месте
  (`startsWith("acp-ui.") && endsWith(".local")` → `ws://${host поменять acp-ui. на acp.}/`),
  т.е. на `acp-ui.mytecor-homelab.local` клиент подключается к `ws://acp.mytecor-homelab.local/` —
  ровно форма транспорта, подтверждённая на шаге 2 (без subprotocol, `hydra-acp 0.1.183`).

**Частичный чек шага 6 (граница trusted LAN / утечка токена):** в прод-бандле из store не найдено
Hydra-токена, cloudflare/api-key паттернов или захардкоженного endpoint/хост-
`VITE_ACP_ENDPOINT` (minifier выкинул unset-override как dead code). Клиентский ingress не
публикует daemon напрямую — UI-клиент ходит только на `ws://acp.<node>.local/` через тот же Caddy
ingress, что и Ferngeist (граница trusted LAN f8-06 сохранена). `nix flake check --all-systems
--no-build` зелёный, включая контракт-тест `tests/app-services.nix` (ассерты acp-ui site,
SPA-fallback, mdns-юниты).

## Проверка клиента в браузере (ручная, 2026-09-20)

Оператор открыл `http://acp-ui.mytecor-homelab.local` в браузере той же LAN и подтвердил, что
клиент загружается и работает (`проверил, … работает`). Это закрывает браузерную загрузку
(load-check) client-части step 5. Полноценный acceptance по контракту f8-06 (≥2 параллельные
сессии, reconnect с `session/list` + `session/attach`, два клиента одной live-сессии, стриминг с
глобальным [`acp-normalizer`](../../packages/acp-normalizer/README.md)) остаётся как углублённая
проверка поверх подтверждённой загрузки, до полного закрытия задачи.

## Открытые вопросы

- Хостинг статики выбран: derivation-пакет `pkgs.lattice.acp-web` с `root *` + `try_files
  {path} /index.html` + `file_server` в Caddy (LAN-only `http://acp-ui.<node>.local`); `plain
  Caddy root` не использован, так как пакет даёт воспроизводимый store-path из закреплённого
  источника.
