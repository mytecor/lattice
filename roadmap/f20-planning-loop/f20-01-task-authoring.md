# Создавать `task.md` из ACP conversation

Фича: [F20 — planning loop](./README.md). Зависит от task contract F11.

## Что сделать

- [ ] На existing ACP ingress уточнять context, requirements и acceptance criteria.
- [ ] Формировать валидный Markdown/YAML contract без runtime state.
- [ ] Commit/push новую task revision в configured task repository.
- [ ] Не выдавать planning agent credentials или API для прямого вызова `agentd`/r1s.

## Критерий готовности

- [ ] Единственный durable output planning session — Git commit с валидным `task.md`.

## Затрагиваемые файлы / слои

- planning agent profile/prompt
- Git authoring integration
- contract tests
