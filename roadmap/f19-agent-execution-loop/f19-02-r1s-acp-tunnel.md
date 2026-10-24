# Подключить ACP через r1s tunnel

Фича: [F19 — agent execution loop](./README.md). Зависит от f19-01 и F10.

## Что сделать

- [x] Запускать logical r1s run через public library API без shell-out, если API достаточен.
- [x] Открывать tunnel к `ACP_PORT` назначенного execution.
- [x] Создавать новую ACP session и отправлять task context обычным ACP client.
- [x] При переназначении logical run закрывать старое соединение и создавать новую session.

## Критерий готовности

- [x] Реальный agent container выполняет turn по ACP; Lattice-specific Pi RPC отсутствует.

## Затрагиваемые файлы / слои

- `packages/agentd/`
- r1s library integration
- ACP integration tests

## Реализация (2026-10-25)

- [R1SRuntime](../../packages/agentd/internal/runtime/r1s.go) использует public API
  `github.com/mytecor/r1s/client`, закреплённый на том же `a5f22a1`, что и
  [пакет r1s](../../packages/r1s/package.nix). Каждый request владеет ephemeral client,
  logical run и tunnel; shell-out в r1s CLI отсутствует.
- [ACP client](../../packages/agentd/internal/acp/websocket.go) выполняет WebSocket handshake,
  `initialize`, `session/new` и `session/prompt` поверх tunnel stream без host TCP listener.
  Используется отдельный случайный ACP token на run. Клиент не предоставляет host filesystem
  или terminal capabilities; tool permission ограничивается `allow_once`.
- Смена execution закрывает прежнее соединение; новая session получает исходный task context.
  Обычный transport error не повторяет turn. До готовности listener повторяется только handshake,
  когда ни один turn ещё не отправлен.
- [NixOS-модуль](../../modules/agentd/README.md) включает backend через `runtime = "r1s"`;
  cluster key и необязательная agent environment передаются через systemd credentials.
  `dummy` остаётся backend по умолчанию.
- Полное восстановление Git/verifier context, `max_attempts`, удалённые checks и получение
  source result остаются в [f19-03](./f19-03-verification-recovery.md).
  [PendingRemote](../../packages/agentd/internal/verifier/remote.go) возвращает явную ошибку
  после remote turn: checks не запускаются на хосте и задача не объявляется completed.

## Проверки

- [Runtime/ACP integration](../../packages/agentd/internal/runtime/r1s_test.go): реальный
  WebSocket/JSON-RPC поверх stream adapter, task inputs, смена execution посреди turn,
  новая session с task context, отмена и cleanup.
- [ACP tests](../../packages/agentd/internal/acp/websocket_test.go): notifications,
  одноразовые permissions, bootstrap readiness и отсутствие replay при обычном обрыве.
- [Module contract](../../tests/agentd.nix): обязательный cluster key для backend r1s,
  credential wiring и сгенерированный startup script.
- [Опциональная live-приёмка](../../packages/agentd/internal/runtime/r1s_live_test.go):
  реальный Pi/hydra OCI image, временный source repo и локальный LLM fixture без внешней сети.
  Запуск требует `R1S_LIVE_IMAGE`, `R1S_LIVE_CLUSTER`, `R1S_LIVE_BOOTSTRAP`;
  `R1S_LIVE_RESOURCE_CLASS` позволяет выделить отдельный allocator.

## Обнаруженные ограничения среды

На homelab текущий рабочий allocator возвращает `permission denied` при записи execution
metadata под своим state directory. Приёмка использует временный allocator с отдельными
state, resource class и containerd namespace. Внешняя сеть в disposable namespace отсутствует:
обычный clone GitHub и обращение к внешнему gateway требуют отдельного решения в execution fabric.
Эти ограничения не обходятся предоставлением host network или host filesystem контейнеру.

## Результат live-приёмки (2026-10-25)

`TestR1SContainerTurn` прошёл на `mytecor-homelab`: public r1s run → tunnel stream →
WebSocket → `initialize` → `session/new` → `session/prompt` → текст `OK` и `end_turn`.
Использован опубликованный Pi/hydra image с CID
`bafkreidu7xqz7om44f7ldmaite6f5ofcwbpwpg3uth7m6niizr36ivdhcq`, bootstrap
`/nix/store/dxhhg2yp56f65fpnvz0xwd6z765hzv4h-agent-runtime-bootstrap/bin/agent-runtime-bootstrap`.
Source repo и LLM fixture подготовлены тестовым entrypoint внутри disposable image;
production bootstrap запускается после подготовки. Provider credentials и host mounts не нужны.
Переназначение посреди turn подтверждено integration-тестом с подменой allocator/tunnel.
Полная production single-agent приёмка с внешним Git и LLM остаётся в
[f19-04](./f19-04-end-to-end-acceptance.md).

Проверки реализации прошли: `go test -race ./...`, `go vet ./...`,
`nix flake check --all-systems --no-build`, `checks.x86_64-linux.agentd` и Nix-сборка
`packages.x86_64-linux.agentd` на homelab (включая package tests). `lychee` — без ошибок.
Временные allocator/state/containerd namespace удалены; рабочие `worker-runtime` и
`rns-server` остались active.
