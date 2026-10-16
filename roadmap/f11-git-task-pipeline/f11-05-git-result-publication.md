# Публиковать terminal result в Git

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-01, f11-03 и f11-04.

## Контекст

`TaskResult` становится durable только после Git commit. Публикация должна учитывать конкурентное
изменение task path и неизвестный исход push, не вводя controller database.

## Что сделать

- [x] Записать `result.status` и `result.revision` в YAML front matter исходного task path.
- [x] Добавлять человекочитаемые `## Result` и `## Verification` без потери постановки.
- [x] Проверять, что source/result revision достижимы из разрешённых repositories.
- [x] Перед commit сравнивать актуальную task revision с обработанной immutable revision.
- [x] Reconcile неизвестный исход commit/push повторным чтением Git.
- [x] При несовместимом конкурентном изменении вернуть явный conflict/blocked, не перезаписывая
      чужой commit.

## Критерий готовности

- [x] Completed, failed и blocked results однозначно читаются из Git и больше не actionable.
- [x] Повторная публикация эквивалентного result идемпотентна; конфликт не скрывается.

## Затрагиваемые файлы / слои

- taskd Git publication (`packages/taskd/gitpublisher.go`)
- task contract package (`packages/task-contract/format.go`)
- integration/fault-injection tests (`packages/taskd/gitpublisher_test.go`)

## Результаты приёмки (2026-10-17)

1. Реализован `GoGitPublisher` в `packages/taskd/gitpublisher.go`.
2. Результат исполнения записывается в YAML front matter исходного `task.md` (`status`, `revision`) с сохранением всех исходных разделов и добавлением `## Result` и `## Verification`.
3. Повторная публикация эквивалентного результата проверена тестом `TestPublishResultIdempotent`: результат идемпотентен, повторные коммиты не создаются.
4. Конкурентное изменение требований к задаче во время её исполнения распознаётся (`TestPublishResultConcurrentModificationConflict`), предотвращая перезапись свежих требований устаревшим результатом.

## Открытые вопросы

_нет_.
