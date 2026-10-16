# git-watchd

`git-watchd` — демон-адаптер между локальными Git-репозиториями и шиной сообщений Meshbus.
Сервис не реализует очередь задач, базу данных событий или персистентные оффсеты: Git выступает
в качестве неизменяемого лога событий, а commit SHA — в качестве курсора.

Подробнее об архитектуре: [TASK_EXECUTION.md](../../TASK_EXECUTION.md) и
[f11-02-git-watchd.md](../../roadmap/f11-git-task-pipeline/f11-02-git-watchd.md).

## Сборка и тесты

```sh
go build ./...
go test -v ./...
```

## Контракты RPC (Meshbus)

### `git.head`

Запрос:
```json
{
  "id": "req-1",
  "repo": "/var/lib/my-repo",
  "ref": "refs/heads/main",
  "reply_to": "git.reply.req-1"
}
```

Ответ:
```json
{
  "id": "req-1",
  "revision": "0123456789abcdef0123456789abcdef01234567"
}
```

### `git.watch`

Запрос (long-poll):
```json
{
  "id": "req-2",
  "repo": "/var/lib/my-repo",
  "ref": "refs/heads/main",
  "after_revision": "0123456789abcdef0123456789abcdef01234567",
  "timeout_ms": 30000,
  "reply_to": "git.reply.req-2"
}
```

Ответ при наличии изменений:
```json
{
  "id": "req-2",
  "status": "changed",
  "from": "0123456789abcdef0123456789abcdef01234567",
  "to": "89abcdef0123456789abcdef0123456789abcdef",
  "changed_paths": ["tasks/task-1.md"]
}
```

Ответ по истечении таймаута:
```json
{
  "id": "req-2",
  "status": "no_change",
  "revision": "0123456789abcdef0123456789abcdef01234567"
}
```
