# ACP normalization transformer for hydra-acp

`acp-normalizer` — Lattice-owned Hydra transformer, the fix for what the client
side of the f8-06 stack calls «rendered torn model answers».

## Проблема

`hydra-acp` 0.1.183 (сам daemon, функция `recordAndBroadcast`/`wp`) даёт каждому
`agent_message_chunk` свежий `messageId`. Клиент, который ключует отрисовку по
`messageId` (например `superlite`), считает каждый чанк отдельным сообщением и
печатает «P», «ong», «!» на отдельных строках вместо «Pong!».

## Почему именно transformer, а не патч daemon

Transformer перехватывает `response:session/update` **до** broadcast клиентам и
может вернуть модифицированный envelope через `{ action: "continue", payload }`.
Это стандартный механизм Hydra и не требует форка. При этом **нельзя просто
удалить** `messageId`: `recordAndBroadcast` после transformer-цепочки заново
инжектит id в любой записываемый update без него. Поэтому нормализатор
**переприсваивает** всем чанкам одного логического сообщения id первого чанка
(или minted UUID, если агент стримит без id) — `wp` такой id не трогает.

## Как работает

- Подписывается на `hydra-acp/transformer/initialize { intercepts: ["response:session/update"] }`.
- На `hydra-acp/transformer/message` (phase `response`, method `session/update`):
  - **границей** является любое событие, не являющееся `agent_message_chunk` /
    `agent_thought_chunk`: и классические (`prompt_received`, `tool_call`,
    `user_message_chunk`, `agent_message`, `agent_thought`, `turn_complete`),
    и inter-turn-шум, который live-поток реально доставляет между ходами
    (`usage_update`, `session_info_update`, `available_commands_update`,
    `_hydra_current_model_update`). Граница сбрасывает текущий логический id
    сессии;
  - только `agent_message_chunk` / `agent_thought_chunk` считаются чанками
    ОДНОГО логического сообщения и склеиваются под общий id.

Правило «всё, что не чанк, — граница» выбрано осознанно вместо фиксированного
списка границ (вариант v0.1.0). Fix-list в v0.1.0 не видел inter-turn-`usage_update`/
`session_info_update` между ходами, поэтому его текущий id перетекал из хода N
в ход N+1, и каждый ход многошаговой сессии записывался под ОДИН и тот же
`messageId`. Эта коллизия запекалась в историю и ломала подгрузку прошлых
сообщений (`session/load`), см. регрессию в [`normalize.test.mjs`](./normalize.test.mjs).
- Соединение — WebSocket на `HYDRA_ACP_WS_URL` с per-process transformer-токеном
  (`hydra-acp-token.*`); env задаёт daemon при спавне процесса.

## Подключение

Декларативно через [модуль `lattice.pi-acp-daemon`](../../modules/pi-acp-daemon/README.md):

```nix
lattice.pi-acp-daemon = {
  transformers.acp-normalizer.command = [ "${pkgs.lattice.acp-normalizer}/bin/acp-normalizer" ];
  defaultTransformers = [ "acp-normalizer" ];
};
```

`defaultTransformers` применяет нормализатор ко всем новым сессиям без участия
клиента. Для быстрой проверки трансформер можно зарегистрировать на лету через
REST `/v1/transformers` (in-memory; сессия подключает его per-session через
`session/new` с `_meta: { "hydra-acp": { "transformers": ["acp-normalizer"] } }`).
