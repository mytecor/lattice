# Реализовать bounded verification и recovery loop

Фича: [F19 — agent execution loop](./README.md). Зависит от f19-02.

## Текущий статус

Частично реализована в ходе [f19-01](./f19-01-agentd-components.md) (коммит `8f2f90f`).
[ExecutionLoop](../../packages/agentd/internal/loop/loop.go) и
[CommandVerifier](../../packages/agentd/internal/verifier/command.go) выполняют checks,
возвращают feedback в ту же session и ограничивают число итераций. Сейчас
[сервис](../../packages/agentd/main.go) использует dummy runtime/ACP; это ещё не приёмка
реального agent container. Task timeout задаётся в [server.go](../../packages/agentd/server.go),
но `max_attempts` и recovery с новой ACP session не реализованы.

## Что сделать

- [x] После agent turn запускать configured acceptance commands из `TaskSpec`.
- [x] При успехе возвращать `TaskResult(completed)`.
- [x] При ошибке отправлять verifier output как ACP feedback и продолжать ту же session.
- [ ] Ограничить loop через `max_iterations`, `max_attempts` и `timeout`.
- [ ] После потери execution восстановить новую session из task specification, current Git state,
      предыдущих commits и последнего verifier result.
- [x] Не добавлять planner/reviewer loop первой версии.

## Критерий готовности

- [ ] Success, exhausted failure и blocked conflict дают однозначный terminal `TaskResult`.
- [ ] Recovery не зависит от старой ACP session.

## Затрагиваемые файлы / слои

- `packages/agentd/`
- verifier runner
- fault-injection tests

## Подтверждение реализованной части

[Тесты execution loop](../../packages/agentd/internal/loop/loop_test.go) покрывают success,
feedback между turns, exhausted iterations, blocked conflict и отмену по context timeout.
[Тесты verifier](../../packages/agentd/internal/verifier/verifier_test.go) проверяют запуск
команд в workspace и результаты проверок. Приёмка recovery после потери реального execution
остаётся открытой.
