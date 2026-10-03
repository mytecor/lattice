# Подключить ACP через r1s tunnel

Фича: [F19 — agent execution loop](./README.md). Зависит от f19-01 и F10.

## Что сделать

- [ ] Запускать logical r1s run через public library API без shell-out, если API достаточен.
- [ ] Открывать tunnel к `ACP_PORT` назначенного execution.
- [ ] Создавать новую ACP session и отправлять task context обычным ACP client.
- [ ] При переназначении logical run закрывать старое соединение и создавать новую session.

## Критерий готовности

- [ ] Реальный agent container выполняет turn по ACP; Lattice-specific Pi RPC отсутствует.

## Затрагиваемые файлы / слои

- `packages/agentd/`
- r1s library integration
- ACP integration tests
