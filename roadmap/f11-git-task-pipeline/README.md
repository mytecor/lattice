# F11. Git task pipeline

F11 превращает Git revisions с `task.md` в выполнение и возвращает terminal `TaskResult` в Git.
Она состоит из generic `git-watchd`, Git reconciler `taskd` и первого deterministic dummy path до
`agentd`/r1s. Общего controller и durable control-plane storage нет.

Git — единственный durable source of truth для task/result state. Queue, leases, worker registry,
heartbeat, scheduler, event journal и consumer offsets не входят в Lattice. Полная модель описана
в [TASK_EXECUTION.md](../../TASK_EXECUTION.md).

Зависит от [F10](../f10-disposable-worker/README.md). Соответствует
[вехе 11](../../ROADMAP.md#f11-git-task-pipeline).

Задачи: [f11-01](./f11-01-task-contract.md),
[f11-02](./f11-02-git-watchd.md),
[f11-03](./f11-03-taskd.md),
[f11-04](./f11-04-dummy-pipeline.md),
[f11-05](./f11-05-git-result-publication.md),
[f11-06](./f11-06-recovery-acceptance.md).

**Критерий готовности:** новая immutable task revision без terminal result находится startup scan
и через `git.watch`, превращается в `TaskSpec`, проходит deterministic dummy workload через r1s,
а terminal result commit'ится в Git. Restart `git-watchd`, `taskd` или `agentd`, потеря execution и
повторная обработка revision не теряют задачу и не требуют durable runtime state сервисов.

**Осознанно откладываем:** Pi/ACP verification loop — до
[F19](../f19-agent-execution-loop/README.md); planning agent — до
[F20](../f20-planning-loop/README.md).

## Границы

```text
git-watchd  Git repo/ref → revision changes
taskd       revision changes → TaskSpec; TaskResult → Git
agentd      TaskSpec → TaskResult
r1s         OCI workload execution
meshbus     transport between services
```

`git-watchd` ничего не знает о tasks. `taskd` ничего не знает об ACP или r1s execution ID.
`agentd` ничего не знает о Git task repository. Сервисы взаимодействуют через meshbus subjects и
wildcard subscriptions без service-discovery metadata и внутренних HTTP API.
