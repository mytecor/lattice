# Провести acceptance single-agent execution loop

Фича: [F19 — agent execution loop](./README.md). Зависит от f19-03.

## Что сделать

- [ ] Пройти путь `task.md → Git → git-watchd → taskd → agentd → r1s → ACP agent`.
- [ ] Выполнить source change, verifier feedback, исправление в той же session и successful result.
- [ ] Повторить с потерей execution и restart `agentd`.
- [ ] Проверить terminal result commit через `taskd` и отсутствие hidden durable state.

## Критерий готовности

- [ ] Все пункты полного Definition of done из
      [TASK_EXECUTION.md](../../TASK_EXECUTION.md#definition-of-done-вертикали) воспроизводимы.

## Затрагиваемые файлы / слои

- end-to-end tests
- operational runbook
- roadmap status
