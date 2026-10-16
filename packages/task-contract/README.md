# task-contract

`task-contract` — пакет контракта задач и результатов для Git task pipeline Lattice
([F11 — Git task pipeline](../../roadmap/f11-git-task-pipeline/README.md)).

Он реализует спецификацию из [TASK_EXECUTION.md](../../TASK_EXECUTION.md) и задачу
[f11-01-task-contract.md](../../roadmap/f11-git-task-pipeline/f11-01-task-contract.md).

## Назначение

- Определяет `TaskSpec` как неизменяемое (`immutable`) представление конкретной ревизии задачи в Git
  (`task repository + path + Git commit`).
- Определяет `TaskResult` с терминальными состояниями `completed`, `failed` и `blocked`.
- Реализует парсинг и валидацию `task.md` (Markdown с YAML front matter).
- Различает `actionable` ревизию (без терминального результата) и завершённую (`terminal`).
- Жёстко отвергает любые поля runtime-состояния в Git (`state: running`, `worker`, `execution_id`,
  `attempt`, `lease`, `heartbeat` и т.п.).

## Использование из Go

```go
import taskcontract "github.com/mytecor/lattice/task-contract"

// Парсинг задачи для конкретной Git-ревизии:
spec, err := taskcontract.ParseTask("git://example/tasks.git", "tasks/task-01.md", "commit-sha", content)
if err != nil {
    // ошибка валидации или наличие запрещённых runtime-полей
}

if spec.IsActionable() {
    // задача требует выполнения (нет терминального результата)
}
```

## CLI-инструмент `task-validate`

Пакет включает бинарник `task-validate` для быстрой валидации контрактов задач:

```sh
task-validate path/to/task.md
task-validate --json path/to/task.md
```
