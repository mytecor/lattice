# Доказать pipeline на deterministic dummy workload

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-03 и готового F10 runtime.

## Контекст

До Pi/ACP loop нужно отделить корректность Git reconciliation и r1s execution от поведения LLM.
Dummy workload должен давать один и тот же проверяемый эффект для одной task revision.

## Что сделать

- [x] Добавить минимальную границу `agentd`, принимающую `TaskSpec` и запускающую logical r1s run.
- [x] Реализовать workload: clone source repository, записать детерминированный файл, сделать
      commit и вернуть result revision.
- [x] Связать путь `task.md → git-watchd → taskd → agentd → r1s → dummy → TaskResult`.
- [x] Сделать повтор одной task revision корректным при уже существующем эквивалентном commit.
- [x] Не добавлять ACP, planner/reviewer loop или durable agentd state.

## Критерий готовности

- [x] Полный dummy path завершается проверяемым source commit и `TaskResult`.
- [x] Повтор после неизвестного исхода не повреждает Git и возвращает тот же логический результат
      либо явный `blocked` conflict.

## Затрагиваемые файлы / слои

- `packages/agentd/` и `modules/agentd/`
- dummy workload executor
- integration tests

## Результаты приёмки (2026-10-17)

1. Реализован пакет `packages/agentd` и NixOS-модуль `modules/agentd`.
2. Реализован `DummyWorkloadExecutor`, клонирующий исходный репозиторий, запускающий декларативные checks и фиксирующий детерминированный `.lattice-tasks/<id>.json`.
3. Подтверждена идемпотентность повторного выполнения: при обнаружении эквивалентного коммита возвращается существующий commit SHA без создания дубликатов.
4. Конфликтующие изменения на том же пути корректно приводят к состоянию `blocked`.

## Открытые вопросы

_нет_.
