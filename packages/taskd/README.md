# taskd

`taskd` — Git-reconciler между состоянием репозитория задач и слоем исполнения `agentd`.
Демон считывает спецификации задач `task.md` из Git, преобразует их в неизменяемые `TaskSpec`
и отправляет в `agentd` через шину Meshbus.

Подробнее об архитектуре: [TASK_EXECUTION.md](../../TASK_EXECUTION.md) и
[f11-03-taskd.md](../../roadmap/f11-git-task-pipeline/f11-03-taskd.md).

## Свойства и инварианты

- **Git как единственный durable source of truth**: в `taskd` нет локальной БД, персистентной
  очереди, базы офсетов или leases.
- **Восстановление без состояния**: при старте сервис получает актуальный `HEAD` через
  `git-watchd` (`git.head`), сканирует репозиторий и запускает все незавершённые (actionable) задачи.
- **Теряемый mapping исполнения**: сервис хранит в памяти только `task revision → currently running request`.
- **Изоляция слоёв**: `taskd` не знает про r1s execution ID, ACP-сессии, туннели или итерации агента.
  Граница взаимодействия с `agentd` — строго `TaskSpec → TaskResult`.
- **Идемпотентность ревизий**: каждая версия задачи однозначно идентифицируется тройкой
  `(task_repo, path, commit)`. Задачи с записанным terminal `result` не запускаются.

## Сборка и тесты

```sh
go build ./...
go test -v ./...
```

## Meshbus-контракты

### Запрос к `agentd` (`agent.run`)

Запрос:
```json
{
  "id": "req-1",
  "spec": {
    "task_repo": "/var/lib/my-repo",
    "path": "tasks/task-01.md",
    "commit": "0123456789abcdef0123456789abcdef01234567",
    "id": "task-01",
    "source": {
      "repo": "git://example/target.git",
      "revision": "main"
    },
    "runtime": {
      "image": "alpine:latest"
    }
  },
  "reply_to": "taskd.agent.reply.req-1"
}
```

Ответ:
```json
{
  "id": "req-1",
  "result": {
    "status": "completed",
    "revision": "89abcdef0123456789abcdef0123456789abcdef"
  }
}
```
