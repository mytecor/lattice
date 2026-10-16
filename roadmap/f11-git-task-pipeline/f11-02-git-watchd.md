# Реализовать generic `git-watchd`

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-01 только для integration tests;
сам сервис task-agnostic.

## Контекст

`git-watchd` адаптирует `repo/ref` к Git revision changes и не реализует очередь событий. Git
является event log, а revision — cursor.

## Что сделать

- [x] Добавить `packages/git-watchd/` и `modules/git-watchd/`.
- [x] Реализовать meshbus request `git.head { repo, ref } → { revision }`.
- [x] Реализовать long-poll `git.watch { repo, ref, after_revision }`.
- [x] При изменении возвращать `{ from, to, changed_paths[] }`, при server-side timeout —
      `no_change`.
- [x] Обработать недоступный repository/ref, force update и повтор одного cursor явными ошибками
      или детерминированным ответом.
- [x] Не добавлять event database, ack, replay queue, consumer offsets или exactly-once delivery.

## Критерий готовности

- [x] Consumer получает head и следующий diff по revision cursor, а после `no_change` может
      безопасно повторить watch.
- [x] Пакет и модуль не импортируют task, ACP, agentd или r1s contracts.

## Затрагиваемые файлы / слои

- `packages/git-watchd/`
- `modules/git-watchd/`
- meshbus contracts
- integration tests с real Git repository

## Открытые вопросы

_нет_.
