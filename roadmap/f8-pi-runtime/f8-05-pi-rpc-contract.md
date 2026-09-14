# Зафиксировать общий ACP/Pi RPC контракт

Фича: [F8 — интерактивный Pi runtime](./README.md). Зависит от
[f8-04](./f8-04-interactive-acceptance.md). **Закрыта.**

## Контекст

Будущий worker использует тот же runtime. Изначально предполагался отдельный сетевой Pi-native RPC
со своим framing/streaming/exit. По решению локальный Pi TUI не используется, а клиентская работа
идёт через ACP: надёжный сетевой ingress зафиксирован в
[f8-06](./f8-06-network-acp-daemon.md). ACP endpoint владеет sessions и streaming, но не задаёт
execution boundary. В [f10-04](../f10-disposable-worker/f10-04-pi-rpc-runner.md) host-side
`pi-acp` запускает Pi RPC внутри контейнера через `PI_ACP_PI_COMMAND`; второго сетевого протокола
при этом не появляется.

## Что сделать

- [x] Клиентский entry point — закреплённый ACP WebSocket endpoint `ws://acp.<nodename>.local/`
      (см. [f8-06](./f8-06-network-acp-daemon.md)); отдельного сетевого Pi-native request/result
      framing и exit semantics не вводится.
- [x] Сопоставление config: ACP-режим использует тот же Pi package/model/tool profile, что и
      локальный runtime ([f8-02](./f8-02-pi-gateway-config.md),
      [f8-03](./f8-03-reproducible-tool-profile.md),
      [f8-06](./f8-06-network-acp-daemon.md)).
- [x] Smoke-проверка без интерактивной сессии — acceptance-тесты
      [`tests/acp-ingress-smoke.mjs`](../../tests/acp-ingress-smoke.mjs) и
      [`tests/hydra-acp-smoke.mjs`](../../tests/hydra-acp-smoke.mjs) выполняют session/new, prompt
      и session-операции без участия TUI.
- [x] Версия контракта зафиксирована закреплёнными версиями `hydra-acp` / `pi-acp`
      (см. [f8-06](./f8-06-network-acp-daemon.md) и
      [`packages/hydra-acp`](../../packages/hydra-acp/README.md)).

## Критерий готовности

- [x] Один Pi package/config обслуживает интерактивных ACP-клиентов и остаётся source of truth для
      будущего контейнерного выполнения из
      [f10-04](../f10-disposable-worker/f10-04-pi-rpc-runner.md).
- [x] ACP-путь не требует сохранённой Pi session или provider-specific параметров.

## Затрагиваемые файлы / слои

- [`profiles/pi`](../../profiles/pi/README.md)
- `checks/`
- [ARCHITECTURE.md](../../ARCHITECTURE.md)

## Открытые вопросы

_нет_. Решение: Pi TUI не используется; отдельного сетевого Pi RPC endpoint нет. ACP endpoint из
[f8-06](./f8-06-network-acp-daemon.md) — ingress/session plane, а контейнерный Pi runtime —
execution plane F10.

## Источник уточнения

Обсуждение «Контейнерный оркестратор Pi» зафиксировало разделение ACP ingress и контейнерного
execution plane.
