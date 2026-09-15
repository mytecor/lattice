# Собрать полный жизненный цикл worker

Фича: [F10 — disposable worker](./README.md). Зависит от
[f10-01](./f10-01-task-specification.md) и [f10-02](./f10-02-worker-isolation.md).

## Контекст

До controller жизненный цикл запускается вручную через `r1s`-client и локальный `r1sd`-allocator, но
уже должен иметь те же конечные состояния и cleanup semantics. Lattice не реализует capacity
discovery, offers или leases: их обеспечивает r1s. Единственное отличие — отсутствие controller:
назначение запускается вручную и всегда относится к этой ноде.

## Что сделать

- [ ] Определить узкий versioned executor contract для `start`, `status`, `result`, `reattach`,
      `follow-up` и `stop`, отображая его на r1s `request`/`inspect`/`cancel`/`result`/`logs` и не
      копируя `ExecutionOffer`/`ExecutionAssign` в схему Lattice.
- [ ] Реализовать executor поверх клиента r1s: `r1s request` submitting digest-pinned OCI image,
      через локальный `r1sd`-allocator на той же ноде.
- [ ] Реализовать `start → clone → environment → execute → publish → destroy` как явную state flow.
- [ ] Создавать уникальные workspace/run identifiers без переиспользования checkout.
- [ ] Гарантировать cleanup при success, failure, timeout и operator cancellation.
- [ ] Сохранять до уничтожения только объявленные result/artifact references и diagnostics.
- [ ] Сопоставить состояния r1s-execution с `pi-subagents external-job`: `queued`, `running`,
      `completed`, `failed`, `stopped`, `blocked`; повторный `reattach` не должен повторно
      отправлять prompt.

## Критерий готовности

- [ ] Все четыре terminal paths уничтожают workspace и execution state.
- [ ] После завершения на worker host нет данных, обязательных для продолжения task.
- [ ] Замена executor-источника (r1s против обратного single-node fallback) не меняет task schema,
      Pi extension или lifecycle state contract.

## Затрагиваемые файлы / слои

- worker lifecycle service/scripts
- integration checks
- operations documentation

## Открытые вопросы

_нет_.

## Источник решения

Обсуждение «Замена r1s в lattice» зафиксировало тонкий local executor/broker как временную execution
implementation вместо r1s. r1s готов; executor теперь строится поверх клиента r1s, а временный
`LocalExecutor` из плана убран.
