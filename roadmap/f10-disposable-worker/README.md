# F10. Agent runtime

F10 создаёт disposable OCI runtime агента: внутри конкретного r1s execution работает long-lived
ACP endpoint, а `agentd` подключается к нему через r1s tunnel как обычный ACP client. Контейнер
содержит workspace, Git, Pi, `pi-acp` и ACP listener; отдельного Lattice-specific Pi RPC нет.

Execution fabric — готовый [r1s](https://github.com/mytecor/r1s). Lattice не добавляет scheduler,
allocator, worker registry, heartbeat или execution leases. Полная граница вычислительного контура
описана в [TASK_EXECUTION.md](../../TASK_EXECUTION.md).

Зависит от [F8](../f8-pi-runtime/README.md) и caches-части
[F9](../f9-cache-artifact-plane/README.md). Соответствует
[вехе 10](../../ROADMAP.md#f10-agent-runtime).

Задачи: [f10-01](./f10-01-package-r1s.md) и [f10-02](./f10-02-deploy-r1sd.md) закрыты и
сохраняют историю подготовки r1s; открытая реализация —
[f10-04](./f10-04-agent-runtime-acp.md),
[f10-05](./f10-05-agent-credentials.md) и
[f10-06](./f10-06-runtime-acceptance.md).

**Критерий готовности:** r1s запускает immutable agent image; контейнер создаёт workspace из
явных source repository/revision и принимает ACP; клиент подключается через r1s tunnel;
уничтожение контейнера не теряет уникальное durable state и повторный старт возможен только из
объявленных входов.

**Осознанно откладываем:** Git task reconciliation — до
[F11](../f11-git-task-pipeline/README.md); `agentd`, verification и recovery loop — до
[F19](../f19-agent-execution-loop/README.md); nested agents — до
[F21](../f21-multi-agent/README.md).

## Зафиксированные границы

```text
agent container
├── workspace
├── git
├── Pi
├── pi-acp
└── ACP listener
```

- Контейнер получает source repository, source revision, task context и ограниченные
  credentials/capabilities.
- ACP — единственный agent protocol. Host-side ingress F8 и ACP endpoint внутри workload имеют
  разные роли и не делят writable session state.
- Session, workspace и конкретный r1s execution ephemeral. Task/result state хранится в Git.
- Runtime не содержит queue, task state machine, retry scheduler или knowledge о `task.md`.
- Общий image может использоваться интерактивным и unattended flow, но lifetime и persistence
  задаются вызывающей стороной.
