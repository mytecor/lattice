# Зафиксировать общий TUI/RPC контракт Pi

Фича: [F8 — интерактивный Pi runtime](./README.md). Зависит от f8-04. **Закрыта.**

## Контекст

Будущий worker использует тот же runtime. Изначально предполагался отдельный Pi-native RPC с
собственным framing/streaming/exit. По решению локальный Pi TUI не используется, а вся
интерактивная и будущая stateless-работа идёт через ACP: надёжный сетевой путь зафиксирован в
[f8-06](./f8-06-network-acp-daemon.md). Отдельного Pi RPC-контракта не вводится — роль RPC entry
point для F10 выполняет тот же закреплённый ACP WebSocket endpoint, без второго параллельного
протокола.

## Что сделать

- [x] RPC entry point для F10 — закреплённый ACP WebSocket endpoint `ws://acp.<nodename>.local/`
      (см. [f8-06](./f8-06-network-acp-daemon.md)); отдельного Pi-native request/result framing и
      exit semantics не вводится.
- [x] Сопоставление config: ACP-режим использует тот же Pi package/model/tool profile, что и
      локальный runtime (f8-02/f8-03, [f8-06](./f8-06-network-acp-daemon.md)).
- [x] Smoke-проверка без интерактивной сессии — acceptance-тесты
      `tests/acp-ingress-smoke.mjs` и `tests/hydra-acp-smoke.mjs` выполняют session/new, prompt и
      session-операции без участия TUI.
- [x] Версия контракта зафиксирована закреплёнными версиями `hydra-acp` / `pi-acp`
      (см. [f8-06](./f8-06-network-acp-daemon.md) и
      [`packages/hydra-acp`](../../../packages/hydra-acp/README.md)).

## Критерий готовности

- [x] Один Pi package/config обслуживает и интерактивных ACP-клиентов, и (далее, в F10)
      stateless-выполнение через тот же ACP endpoint.
- [x] ACP-путь не требует сохранённой Pi session или provider-specific параметров.

## Затрагиваемые файлы / слои

- `profiles/pi/`
- `checks/`
- `ARCHITECTURE.md`

## Открытые вопросы

_нет_. Решение: Pi TUI не используется; отдельного Pi RPC-контракта нет — роль entry point для
F10 выполняет ACP endpoint из [f8-06](./f8-06-network-acp-daemon.md).
