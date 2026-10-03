# Публиковать terminal result в Git

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-01, f11-03 и f11-04.

## Контекст

`TaskResult` становится durable только после Git commit. Публикация должна учитывать конкурентное
изменение task path и неизвестный исход push, не вводя controller database.

## Что сделать

- [ ] Записать `result.status` и `result.revision` в YAML front matter исходного task path.
- [ ] Добавлять человекочитаемые `## Result` и `## Verification` без потери постановки.
- [ ] Проверять, что source/result revision достижимы из разрешённых repositories.
- [ ] Перед commit сравнивать актуальную task revision с обработанной immutable revision.
- [ ] Reconcile неизвестный исход commit/push повторным чтением Git.
- [ ] При несовместимом конкурентном изменении вернуть явный conflict/blocked, не перезаписывая
      чужой commit.

## Критерий готовности

- [ ] Completed, failed и blocked results однозначно читаются из Git и больше не actionable.
- [ ] Повторная публикация эквивалентного result идемпотентна; конфликт не скрывается.

## Затрагиваемые файлы / слои

- taskd Git publication
- task contract package
- integration/fault-injection tests

## Открытые вопросы

_нет_.
