# Провести end-to-end recovery drill Git task pipeline

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-02–f11-05.

## Контекст

Финальный критерий проверяет, что correctness держится на Git и r1s, а не на памяти сервисов,
доставке события ровно один раз или восстановлении ACP session.

## Что сделать

- [ ] Выполнить обычный dummy path от commit `task.md` до terminal result commit.
- [ ] Повторить с restart `git-watchd`, `taskd` и `agentd` в разных точках.
- [ ] Потерять конкретный r1s execution и проверить logical run/retry на новом execution.
- [ ] Повторно обработать ту же task revision до и после result publication.
- [ ] Удалить весь ephemeral local state сервисов и восстановить actionable set startup scan'ом.
- [ ] Проверить отсутствие потерянных задач и зависимости от queue, lease или worker registry.

## Критерий готовности

- [ ] Во всех сценариях task получает terminal result либо остаётся actionable/явно blocked в Git.
- [ ] Recovery требует только Git и возможностей r1s; ручное восстановление базы сервисов не
      требуется.

## Затрагиваемые файлы / слои

- end-to-end/fault-injection tests
- recovery runbook
- roadmap status

## Открытые вопросы

_нет_.
