# F19. Agent execution loop

F19 развивает минимальный `agentd` из F11 до single-agent execution loop. `agentd` запускает
logical r1s run через public library API, открывает tunnel к ACP endpoint agent container, создаёт
session, отправляет `TaskSpec`, запускает verifier и возвращает terminal `TaskResult`.

Зависит от [F10](./../f10-disposable-worker/README.md) и
[F11](./../f11-git-task-pipeline/README.md). Соответствует
[вехе 19](./../../ROADMAP.md#f19-agent-execution-loop). Архитектурный контракт описан в
[TASK_EXECUTION.md](./../../TASK_EXECUTION.md).

## Текущий статус

F19 остаётся runtime-baseline. Итоговое направление 2026-11-02 описано в
[AGENT_WORKFLOW.md](./../AGENT_WORKFLOW.md): принятие результата в trunk и независимые review/checks
проектируются в [F22](./../f22-git-trunk-review/README.md), права распределённого исполнения — в
[F23](./../f23-distributed-coordination/README.md). Успех ACP turn или terminal `TaskResult` baseline
не подменяет принятие задачи/фичи. Перед завершением оставшихся задач F19 нужно согласовать их
интерфейсы с новым контрактом, не ослабляя существующие runtime-проверки и не утверждая, что
старый прямой pipeline уже реализует целевой процесс.

Сверка с реализацией 2026-10-25: [f19-01](./f19-01-agentd-components.md) и
[f19-02](./f19-02-r1s-acp-tunnel.md) выполнены. В [agentd](./../../packages/agentd/main.go)
доступны `dummy` (по умолчанию) и `r1s` backends. Public r1s API, tunnel и реальный ACP turn
в Pi/hydra image подтверждены live-тестом с локальным LLM fixture; замена session при
переназначении execution покрыта integration-тестом.

Следующий шаг — [f19-03](./f19-03-verification-recovery.md): удалённые checks, source result,
ограничение attempts и полное восстановление task context. Сейчас remote backend после turn
возвращает явную ошибку верификации. У рабочего allocator homelab обнаружена проблема прав
на execution metadata, а у disposable namespace отсутствует внешняя сеть; production
single-agent path требует устранения этих ограничений перед f19-04.

Задачи:

- [f19-01](./f19-01-agentd-components.md) — разделить `agentd` на runtime/acp/loop/verifier;
- [f19-02](./f19-02-r1s-acp-tunnel.md) — r1s logical run и ACP over tunnel;
- [f19-03](./f19-03-verification-recovery.md) — bounded verification и recovery loop;
- [f19-04](./f19-04-end-to-end-acceptance.md) — полная acceptance single-agent path.

**Критерий готовности:** `TaskSpec` приводит к изменениям source repository через disposable ACP
agent; configured checks либо завершают задачу, либо возвращаются агенту как feedback; потеря
execution или `agentd` не требует восстановления ACP session и не вводит durable local state.

**Осознанно откладываем:** planning agent — до [F20](./../f20-planning-loop/README.md), nested agents
— до [F21](./../f21-multi-agent/README.md).
