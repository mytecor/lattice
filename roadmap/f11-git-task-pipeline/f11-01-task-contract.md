# Зафиксировать контракт `task.md` и `TaskResult`

Фича: [F11 — Git task pipeline](./README.md). Зависит от
[F10](../f10-disposable-worker/README.md).

## Контекст

Task/result state целиком хранится в Git. Версия постановки определяется task repository, path и
Git commit; изменение Markdown создаёт новую immutable revision. Runtime state в task contract не
попадает.

## Что сделать

- [ ] Реализовать parser/validation Markdown с YAML front matter для `id`, `source`, `runtime`,
      опциональных `checks` и textual Context/Requirements/Acceptance criteria.
- [ ] Определить `TaskSpec` как immutable представление конкретной task revision.
- [ ] Определить `TaskResult` с terminal states `completed`, `failed`, `blocked` и result revision.
- [ ] Считать revision actionable до появления terminal `result`.
- [ ] Запретить runtime-поля `state: running`, `worker`, `execution`, `attempt`, lease и аналоги.
- [ ] Добавить contract/invariant tests, не привязанные к произвольным значениям конфигурации.

## Критерий готовности

- [ ] Для task repository + path + commit однозначно строится один `TaskSpec`.
- [ ] Parser различает actionable и terminal revision и отвергает runtime state в Git contract.

## Затрагиваемые файлы / слои

- task contract package
- [TASK_EXECUTION.md](../../TASK_EXECUTION.md)
- contract tests

## Открытые вопросы

_нет_.
