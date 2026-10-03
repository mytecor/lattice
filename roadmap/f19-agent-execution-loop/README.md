# F19. Agent execution loop

F19 развивает минимальный `agentd` из F11 до single-agent execution loop. `agentd` запускает
logical r1s run через public library API, открывает tunnel к ACP endpoint agent container, создаёт
session, отправляет `TaskSpec`, запускает verifier и возвращает terminal `TaskResult`.

Зависит от [F10](../f10-disposable-worker/README.md) и
[F11](../f11-git-task-pipeline/README.md). Соответствует
[вехе 19](../../ROADMAP.md#f19-agent-execution-loop). Архитектурный контракт описан в
[TASK_EXECUTION.md](../../TASK_EXECUTION.md).

Задачи:

- [f19-01](./f19-01-agentd-components.md) — разделить `agentd` на runtime/acp/loop/verifier;
- [f19-02](./f19-02-r1s-acp-tunnel.md) — r1s logical run и ACP over tunnel;
- [f19-03](./f19-03-verification-recovery.md) — bounded verification и recovery loop;
- [f19-04](./f19-04-end-to-end-acceptance.md) — полная acceptance single-agent path.

**Критерий готовности:** `TaskSpec` приводит к изменениям source repository через disposable ACP
agent; configured checks либо завершают задачу, либо возвращаются агенту как feedback; потеря
execution или `agentd` не требует восстановления ACP session и не вводит durable local state.

**Осознанно откладываем:** planning agent — до [F20](../f20-planning-loop/README.md), nested agents
— до [F21](../f21-multi-agent/README.md).
