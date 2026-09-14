# Запускать общий Pi runtime внутри контейнера

Фича: [F10 — disposable worker](./README.md). Зависит от
[f8-05](../f8-pi-runtime/f8-05-pi-rpc-contract.md) и
[f10-03](./f10-03-worker-lifecycle.md).

## Контекст

Worker не получает отдельный agent harness. `hydra-acp` и `pi-acp` остаются на host как
ingress/session plane, а `pi-acp` запускает Pi RPC внутри общего контейнерного runtime через
`PI_ACP_PI_COMMAND`. Интерактивный оркестратор и каждый disposable worker получают один Pi
package/config/tool profile/extensions; различаются только task, workspace, capabilities и state
policy.

## Что сделать

- [ ] Настроить host-side `pi-acp` на `PI_ACP_PI_COMMAND=lattice-pi-container`, не патча upstream
      ACP adapter.
- [ ] Реализовать wrapper, который прозрачно передаёт Pi все argv, включая `--version` и
      `--mode rpc`, сохраняет stdin/stdout и exit status.
- [ ] Монтировать session file и workspace/cwd в контейнер по тем же абсолютным путям, которые
      передаёт `pi-acp`; не переписывать ACP session metadata.
- [ ] Разделить state policy: writable persistent session state для оркестратора при resume и
      tmpfs/одноразовая Pi session для disposable worker; runtime/config остаётся общим.
- [ ] Дать контейнерному Pi стабильный разрешённый endpoint LLM gateway вместо host-loopback URL;
      не использовать host networking как обход isolation policy.
- [ ] Добавить к общему runtime `nicobailon/pi-subagents` и Lattice-owned extension
      `pi-lattice-workers`, зарегистрированный как `external-job` provider к
      `/run/lattice/worker.sock`.
- [ ] Запускать через provider полноценный Pi runtime в sibling container, снова с
      `pi-subagents`/`pi-lattice-workers`, чтобы nested agents могли повторять тот же flow без
      доступа к `containerd.sock`.
- [ ] Преобразовать validated task specification во вход Pi RPC без скрытой session state.
- [ ] Потоково собирать events/logs и различать result, tool failure, model failure и timeout.
- [ ] Передавать только logical model class и разрешённый tool profile.
- [ ] Добавить deterministic smoke task с fake gateway и real Git workspace.

## Критерий готовности

- [ ] ACP session оркестратора подтверждает, что реальный `pi --mode rpc` выполняется внутри
      контейнера и использует те же immutable runtime derivations, что и disposable worker.
- [ ] Wrapper проходит version probe, RPC smoke, cwd/session-path и disconnect/resume проверки.
- [ ] Nested subagent создаёт sibling Pi container через broker и возвращает result, не получая
      прямого доступа к container runtime.
- [ ] Pi RPC завершает smoke task и возвращает машинно-читаемый result envelope.
- [ ] Повтор на новом worker не требует данных предыдущей Pi session.

## Затрагиваемые файлы / слои

- worker runner
- [`modules/pi`](../../modules/pi/README.md)
- [`modules/pi-acp-daemon`](../../modules/pi-acp-daemon/README.md)
- [`profiles/pi`](../../profiles/pi/README.md)
- integration checks

## Открытые вопросы

_нет_.

## Источники решения

- Обсуждение «Замена r1s в lattice» — `pi-subagents external-job`, local broker и рекурсивные sibling
  containers.
- Обсуждение «Контейнерный оркестратор Pi» — host-side `pi-acp`, `PI_ACP_PI_COMMAND`, общий runtime и
  прозрачные session/workspace paths.
