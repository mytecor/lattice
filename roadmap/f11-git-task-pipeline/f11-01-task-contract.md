# Зафиксировать контракт `task.md` и `TaskResult`

Фича: [F11 — Git task pipeline](./README.md). Зависит от
[F10](../f10-disposable-worker/README.md).

## Контекст

Task/result state целиком хранится в Git. Версия постановки определяется task repository, path и
Git commit; изменение Markdown создаёт новую immutable revision. Runtime state в task contract не
попадает.

## Что сделать

- [x] Реализовать parser/validation Markdown с YAML front matter для `id`, `source`, `runtime`,
      опциональных `checks` и textual Context/Requirements/Acceptance criteria.
- [x] Определить `TaskSpec` как immutable представление конкретной task revision.
- [x] Определить `TaskResult` с terminal states `completed`, `failed`, `blocked` и result revision.
- [x] Считать revision actionable до появления terminal `result`.
- [x] Запретить runtime-поля `state: running`, `worker`, `execution`, `attempt`, lease и аналоги.
- [x] Добавить contract/invariant tests, не привязанные к произвольным значениям конфигурации.

## Критерий готовности

- [x] Для task repository + path + commit однозначно строится один `TaskSpec`.
- [x] Parser различает actionable и terminal revision и отвергает runtime state в Git contract.

## Затрагиваемые файлы / слои

- [`packages/task-contract`](../../packages/task-contract/README.md)
- [TASK_EXECUTION.md](../../TASK_EXECUTION.md)
- contract tests (`packages/task-contract/contract_test.go`, [`tests/task-contract.nix`](../../tests/task-contract.nix))

## Открытые вопросы

_нет_.
