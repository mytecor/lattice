# F20. Planning loop

Planning agent работает через существующий ACP ingress Lattice и превращает разговор, уточнения и
acceptance criteria в Git commit с `task.md`. Он не вызывает `agentd` или r1s напрямую; pipeline
запускается самим появлением новой Git revision.

Зависит от [F11](../f11-git-task-pipeline/README.md) и
[F19](../f19-agent-execution-loop/README.md). Соответствует
[вехе 20](../../ROADMAP.md#f20-planning-loop). Контракт описан в
[TASK_EXECUTION.md](../../TASK_EXECUTION.md#planning-и-multi-agent).

Задачи:

- [f20-01](./f20-01-task-authoring.md) — создать `task.md` из ACP conversation;
- [f20-02](./f20-02-end-to-end-acceptance.md) — принять conversation-to-result path.

**Критерий готовности:** planning agent commit'ит валидную task revision, после чего F11/F19 без
прямого вызова со стороны planner доводят её до terminal result в Git.

**Осознанно откладываем:** multi-agent orchestration — до
[F21](../f21-multi-agent/README.md).
