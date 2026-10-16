# git-watchd

`git-watchd` — generic-адаптер между локальными Git-репозиториями и шиной сообщений Meshbus.
Сервис реализует контракт наблюдения за ревизиями репозитория без собственной базы данных,
очереди событий, ack-протокола или персистентных оффсетов консьюмеров.

Архитектура описана в [TASK_EXECUTION.md](../../TASK_EXECUTION.md), а задача создания — в
[f11-02-git-watchd.md](../../roadmap/f11-git-task-pipeline/f11-02-git-watchd.md).

## Контракт Meshbus

Сервис слушает два subject:
- `git.head`: запрос текущего commit SHA для `{ repo, ref }`.
- `git.watch`: long-poll запрос изменений после указанного курсора `after_revision`. При наличии
  изменений возвращает `{ status: "changed", from, to, changed_paths[] }`, по истечении таймаута
  возвращает `{ status: "no_change", revision }`.

Сервис работает исключительно с локальными репозиториями на файловой системе ноды. Удалённые
репозитории синхронизируются внешними механизмами (например, через
[git-cache-proxy](../git-cache-proxy/README.md), Radicle или git-хуки).

## Параметры конфигурации

- `lattice.git-watchd.enable`: включение сервиса.
- `lattice.git-watchd.realmKeyFile`: путь к файлу с 32-байтным ключом Meshbus realm.
- `lattice.git-watchd.allowedDir`: опциональный префикс разрешённых директорий репозиториев.
- `lattice.git-watchd.headSubject`: subject для запросов HEAD (по умолчанию `git.head`).
- `lattice.git-watchd.watchSubject`: subject для запросов наблюдения (по умолчанию `git.watch`).
- `lattice.git-watchd.pollInterval`: интервал опроса локальных ref при long-poll (по умолчанию `250ms`).
