# Lattice agentd

`agentd` — сервис исполнения задач в Lattice ([TASK_EXECUTION.md](../../TASK_EXECUTION.md)).
Он слушает запросы `TaskSpec` на шине Meshbus (`agent.run`), исполняет рабочую нагрузку
через r1s/dummy workload, проверяет детерминированные результаты и возвращает terminal `TaskResult`.

## Архитектура

`agentd` не знает о Git task repository, пользователях или durable очередях. Он работает строго по контракту:

```text
TaskSpec → Executor → TaskResult
```

- В F11 детерминированный dummy workload клонирует source repository, записывает
  детерминированный файл `.lattice-tasks/<id>.json`, выполняет checks (если заданы),
  создаёт коммит и возвращает commit SHA.
- Повторное исполнение той же task revision идемпотентно: существующий эквивалентный коммит
  обнаруживается и возвращается без дублирования.
- Конфликт содержимого приводит к terminal status `blocked`.

## Флаги

- `--realm-key-file`: путь к 32-байтному ключу Meshbus realm.
- `--agent-subject`: Meshbus subject (по умолчанию `agent.run`).
- `--work-dir`: директория для временных рабочих деревьев.
