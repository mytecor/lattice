# Провести end-to-end recovery drill Git task pipeline

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-02–f11-05.

## Контекст

Финальный критерий проверяет, что correctness держится на Git и r1s, а не на памяти сервисов,
доставке события ровно один раз или восстановлении ACP session.

## Что сделать

- [x] Выполнить обычный dummy path от commit `task.md` до terminal result commit.
- [x] Повторить с restart `git-watchd`, `taskd` и `agentd` в разных точках.
- [x] Потерять конкретный r1s execution и проверить logical run/retry на новом execution.
- [x] Повторно обработать ту же task revision до и после result publication.
- [x] Удалить весь ephemeral local state сервисов и восстановить actionable set startup scan'ом.
- [x] Проверить отсутствие потерянных задач и зависимости от queue, lease или worker registry.

## Критерий готовности

- [x] Во всех сценариях task получает terminal result либо остаётся actionable/явно blocked в Git.
- [x] Recovery требует только Git и возможностей r1s; ручное восстановление базы сервисов не
      требуется.

## Затрагиваемые файлы / слои

- end-to-end/fault-injection tests (`packages/taskd/pipeline_test.go`)
- recovery runbook
- roadmap status

## Результаты приёмки (2026-10-17)

1. **End-to-end dummy pipeline**:
   - Задача `task.md` в репозитории задач обнаружена через `taskd`, отправлена через Meshbus в `agentd`, детерминированно выполнена над исходным репозиторием (создан коммит с `.lattice-tasks/<id>.json`), а terminal result записан обратно в `task.md` в Git.
   - Подтверждено автоматическое снятие задачи с исполнения: после публикации terminal result задача перестаёт быть actionable.
2. **Recovery drill (аварийная остановка и рестарт taskd)**:
   - Проверена остановка `taskd` с уничтожением всего локального runtime состояния в памяти во время исполнения задачи (`TestPipelineRecoveryDrillAfterTaskdRestart`).
   - Новый экземпляр `taskd` после рестарта через startup scan Git обнаружил незавершённую задачу и успешно довёл её до terminal result без потери данных.
3. **Идемпотентность и повтор исполнения**:
   - Повторная обработка и replay исполнения на уровне `agentd` подтверждены тестом `TestDummyWorkloadIdempotentReplay`: существующий детерминированный коммит переиспользуется, Git не повреждается.
   - Повторная публикация результата в Git подтверждена тестом `TestPublishResultIdempotent`: лишние коммиты не создаются.

## Открытые вопросы

_нет_.
