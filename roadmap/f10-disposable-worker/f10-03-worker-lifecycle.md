# Собрать полный жизненный цикл worker

Фича: [F10 — disposable worker](./README.md). Зависит от
[f10-01](./f10-01-task-specification.md) и [f10-02](./f10-02-worker-isolation.md).

## Контекст

До controller жизненный цикл запускается вручную через тонкий local broker/`LocalExecutor`, но уже
должен иметь те же конечные состояния и cleanup semantics. Этот слой не реализует scheduler,
capacity discovery, offers или leases r1s: локальное назначение неявно и всегда относится к этой
ноде.

## Что сделать

- [ ] Определить узкий versioned executor contract для `start`, `status`, `result`, `reattach`,
      `follow-up` и `stop`, не копируя `ExecutionOffer`/`ExecutionAssign` из r1s.
- [ ] Реализовать `LocalExecutor` поверх `containerd` и Unix-socket broker
      `/run/lattice/worker.sock`.
- [ ] Реализовать `start → clone → environment → execute → publish → destroy` как явную state flow.
- [ ] Создавать уникальные workspace/run identifiers без переиспользования checkout.
- [ ] Гарантировать cleanup при success, failure, timeout и operator cancellation.
- [ ] Сохранять до уничтожения только объявленные result/artifact references и diagnostics.
- [ ] Сопоставить состояния broker с `pi-subagents external-job`: `queued`, `running`, `completed`,
      `failed`, `stopped`, `blocked`; повторный `reattach` не должен повторно отправлять prompt.

## Критерий готовности

- [ ] Все четыре terminal paths уничтожают workspace и execution state.
- [ ] После завершения на worker host нет данных, обязательных для продолжения task.
- [ ] Замена `LocalExecutor` на будущий r1s-adapter не меняет task schema, Pi extension или
      lifecycle state contract.

## Затрагиваемые файлы / слои

- worker lifecycle service/scripts
- integration checks
- operations documentation

## Открытые вопросы

_нет_.

## Источник решения

Обсуждение «Замена r1s в lattice» зафиксировало тонкий local executor/broker как временную execution
implementation вместо r1s.
