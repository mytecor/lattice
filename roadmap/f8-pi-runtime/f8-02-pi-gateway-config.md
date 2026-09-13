# Подключить Pi к логическим моделям gateway

Фича: [F8 — интерактивный Pi runtime](./README.md). Зависит от
[f8-01](./f8-01-package-pi.md) и F7.

## Контекст

Pi не должен знать provider endpoints, credentials или реальные model IDs. Ему доступны только
gateway URL, client credential и логические классы. Активный контракт F7 сокращён до **двух**
классов `stupid` и `standard` (расхождение с прежней формулировкой «четыре класса» отражено в
[Открытые вопросы](#открытые-вопросы)).

Логические классы и base URL задаются декларативно в Nix; provider-specific discovery отключён,
upstream credentials остаются внутри `llm-gateway` и в конфигурацию Pi не попадают.

## Что сделать

- [x] Сгенерировать декларативную Pi-конфигурацию с OpenAI-compatible gateway endpoint.
- [x] Выдать Pi отдельный client credential через agenix/runtime boundary — закрыто: client auth в gateway выключен, Pi заходит только по loopback, поэтому отдельный client credential не требуется (см. «Реализация»).
- [x] Отключить provider-specific auto-discovery и перечислить только логические классы.
- [ ] Проверить streaming и переключение класса модели в TUI (f8-04).

## Критерий готовности

- [x] В Pi нет provider-specific конфигурации или upstream credentials.
- [ ] Логические модели `standard`/`stupid` работают через gateway в streaming-режиме
      (проверяется в f8-04 на интерактивной TUI-сессии).

## Затрагиваемые файлы / слои

- `modules/pi/` — декларативные опции `settings`/`models` и генерация store JSON.
- `nodes/mytecor-homelab/` — привязка Pi к loopback gateway и логическим классам.
- `tests/pi-config.nix` — NixOS-проверка материализации конфига.
- `KEY_MANAGEMENT.md` — client credential workflow (когда включим client auth).

## Открытые вопросы

- **Client credential** (закрыто 2026-09-09): gateway работает **без client auth**
  (`clientCredentialFile = null`), а Pi подключается только по loopback
  `127.0.0.1:9208/v1` на той же ноде — внутренний порт, который в firewall не открыт.
  Поэтому отдельный client credential не требуется и не выдаётся. Когда в будущем включим
  `clientCredentialFile`, `apiKey` у provider задаётся env-ссылкой, не литералом в store.
  Порт `9208` берётся из общего каталога портов `profiles/networking/ports.nix`.
- **«Четыре класса» в критерии** расходится с активным набором F7: сейчас только `stupid`
  и `standard`. Формулировка приведена к фактическим двум классам.

## Реализация

Завершено 2026-09-08 (конфиг; интерактивная проверка — f8-04). Пункт client credential
закрыт 2026-09-09: client auth в gateway выключен, соединение loopback-only, поэтому
отдельный credential не требуется; порт `9208` един в `profiles/networking/ports.nix`.
`modules/pi` расширен опциями
`lattice.pi.settings` и `lattice.pi.models`: генераторы создают immutable JSON в Nix store
(`generatedSettingsJson`/`generatedModelsJson`), а activation script материализует
`~/.pi/agent/settings.json` и `~/.pi/agent/models.json` как symlink на store-файлы; каталог
`~/.pi/agent` остаётся writable для runtime-состояния Pi. В node-конфиг добавлена привязка к
`llm-gateway` по loopback: `discoverModels = false`, `models = [{id=standard},{id=stupid}]`,
без `apiKey` (секреты в store не попадают). `tests/pi-config.nix` проверяет симлинки и
отсутствие provider-specific discovery/credentials. Streaming и переключение класса модели
проверяются в f8-04.
