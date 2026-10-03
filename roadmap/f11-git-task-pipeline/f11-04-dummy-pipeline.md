# Доказать pipeline на deterministic dummy workload

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-03 и готового F10 runtime.

## Контекст

До Pi/ACP loop нужно отделить корректность Git reconciliation и r1s execution от поведения LLM.
Dummy workload должен давать один и тот же проверяемый эффект для одной task revision.

## Что сделать

- [ ] Добавить минимальную границу `agentd`, принимающую `TaskSpec` и запускающую logical r1s run.
- [ ] Реализовать workload: clone source repository, записать детерминированный файл, сделать
      commit и вернуть result revision.
- [ ] Связать путь `task.md → git-watchd → taskd → agentd → r1s → dummy → TaskResult`.
- [ ] Сделать повтор одной task revision корректным при уже существующем эквивалентном commit.
- [ ] Не добавлять ACP, planner/reviewer loop или durable agentd state.

## Критерий готовности

- [ ] Полный dummy path завершается проверяемым source commit и `TaskResult`.
- [ ] Повтор после неизвестного исхода не повреждает Git и возвращает тот же логический результат
      либо явный `blocked` conflict.

## Затрагиваемые файлы / слои

- минимальный `packages/agentd/` и `modules/agentd/`
- dummy OCI workload
- r1s library integration
- end-to-end tests

## Открытые вопросы

_нет_.
