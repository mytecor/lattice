# Реализовать generic `git-watchd`

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-01 только для integration tests;
сам сервис task-agnostic.

## Контекст

`git-watchd` адаптирует `repo/ref` к Git revision changes и не реализует очередь событий. Git
является event log, а revision — cursor.

## Что сделать

- [ ] Добавить `packages/git-watchd/` и `modules/git-watchd/`.
- [ ] Реализовать meshbus request `git.head { repo, ref } → { revision }`.
- [ ] Реализовать long-poll `git.watch { repo, ref, after_revision }`.
- [ ] При изменении возвращать `{ from, to, changed_paths[] }`, при server-side timeout —
      `no_change`.
- [ ] Обработать недоступный repository/ref, force update и повтор одного cursor явными ошибками
      или детерминированным ответом.
- [ ] Не добавлять event database, ack, replay queue, consumer offsets или exactly-once delivery.

## Критерий готовности

- [ ] Consumer получает head и следующий diff по revision cursor, а после `no_change` может
      безопасно повторить watch.
- [ ] Пакет и модуль не импортируют task, ACP, agentd или r1s contracts.

## Затрагиваемые файлы / слои

- `packages/git-watchd/`
- `modules/git-watchd/`
- meshbus contracts
- integration tests с real Git repository

## Открытые вопросы

_нет_.
