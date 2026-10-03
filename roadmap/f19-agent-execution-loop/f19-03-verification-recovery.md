# Реализовать bounded verification и recovery loop

Фича: [F19 — agent execution loop](./README.md). Зависит от f19-02.

## Что сделать

- [ ] После agent turn запускать configured acceptance commands из `TaskSpec`.
- [ ] При успехе возвращать `TaskResult(completed)`.
- [ ] При ошибке отправлять verifier output как ACP feedback и продолжать ту же session.
- [ ] Ограничить loop через `max_iterations`, `max_attempts` и `timeout`.
- [ ] После потери execution восстановить новую session из task specification, current Git state,
      предыдущих commits и последнего verifier result.
- [ ] Не добавлять planner/reviewer loop первой версии.

## Критерий готовности

- [ ] Success, exhausted failure и blocked conflict дают однозначный terminal `TaskResult`.
- [ ] Recovery не зависит от старой ACP session.

## Затрагиваемые файлы / слои

- `packages/agentd/`
- verifier runner
- fault-injection tests
