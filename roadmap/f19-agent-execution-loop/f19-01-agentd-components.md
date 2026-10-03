# Разделить `agentd` на внутренние components

Фича: [F19 — agent execution loop](./README.md). Зависит от dummy `agentd` в
[f11-04](../f11-git-task-pipeline/f11-04-dummy-pipeline.md).

## Что сделать

- [ ] Выделить packages/components `runtime`, `acp`, `loop` и `verifier` внутри одного сервиса.
- [ ] Сохранить внешнюю границу только как `TaskSpec → TaskResult` через meshbus.
- [ ] Не превращать components в отдельные сервисы и не добавлять общий controller.
- [ ] Ограничить runtime state теряемыми mappings активных requests.

## Критерий готовности

- [ ] Границы components покрыты contract tests; restart не требует локальной базы.

## Затрагиваемые файлы / слои

- `packages/agentd/`
- `modules/agentd/`
- contract tests
