# F21. Multi-agent

F21 добавит nested ephemeral agents после завершения single-agent pipeline. Root agent запускает
child workload через r1s и общается с ним по ACP over r1s tunnel.

Зависит от [F19](../f19-agent-execution-loop/README.md). Соответствует
[вехе 21](../../ROADMAP.md#f21-multi-agent). Ограничения описаны в
[TASK_EXECUTION.md](../../TASK_EXECUTION.md#planning-и-multi-agent).

Задачи будут детализированы перед началом реализации после измерения single-agent path.

**Критерий готовности:** root agent запускает и завершает nested child workloads без прямого
доступа к allocator/container runtime; children общаются по ACP и не требуют durable Lattice
control state.

**Архитектурные ограничения:** `parent_id` не добавляется в task/control state; child живёт не
дольше владеющего root process/run. Работа, которая должна пережить root, оформляется новой
durable `task.md`, а не долгоживущим subagent.
