# Lattice agentd

`agentd` — сервис исполнения задач в Lattice ([TASK_EXECUTION.md](../../TASK_EXECUTION.md)).
Он слушает запросы `TaskSpec` на шине Meshbus (`agent.run`), исполняет рабочую нагрузку
через выбранный backend и возвращает `TaskResult`. `dummy` выполняет deterministic workload;
`r1s` запускает реальный disposable ACP agent через public library API и tunnel.
Удалённые проверки и source result collection ещё относятся к
[f19-03](../../roadmap/f19-agent-execution-loop/f19-03-verification-recovery.md): после remote turn
сейчас возвращается явная ошибка верификации, без запуска checks на хосте и ложного completed.

## Архитектура и компоненты

`agentd` не знает о Git task repository, пользователях или durable очередях. Он работает строго по контракту:

```text
TaskSpec → ExecutionLoop → TaskResult
```

Внутри одного сервиса код разделён на четыре внутренних пакета:

1. **`internal/runtime`** (`Runtime`, `Instance`): отвечает за подготовку и жизненный цикл окружения исполнения (клонирование/подготовка репозитория в dummy runtime; в f19-02 — запуск logical r1s run).
2. **`internal/acp`** (`Client`, `Session`): абстрагирует ACP-взаимодействие с агентом внутри рабочей нагрузки (инициализация сессии, отправка инструкций и фидбека, ожидание turn result).
3. **`internal/verifier`** (`Verifier`): выполняет декларативные проверки (`spec.Checks`) в рабочем дереве и возвращает структурированный `Result` с деталями упавших проверок.
4. **`internal/loop`** (`ExecutionLoop`): координирует ограниченный цикл выполнения single-agent:
   - Запускает `Runtime`
   - Инициализирует `Session` в `ACP`
   - Отправляет задачу агенту
   - Запускает `Verifier`
   - При успехе возвращает `TaskResult` со статусом `completed`
   - При ошибке проверок возвращает вывод верификатора агенту как ACP-фидбек на следующую итерацию
   - При исчерпании лимита итераций (`max_iterations`) возвращает статус `failed`

Сервис не сохраняет локального состояния между перезапусками: активные запросы отслеживаются только в оперативной памяти (`activeRuns`) и прерываются по context cancellation.

## Флаги

- `--realm-key-file`: путь к 32-байтному ключу Meshbus realm.
- `--agent-subject`: Meshbus subject (по умолчанию `agent.run`).
- `--work-dir`: директория для временных рабочих деревьев.
- `--max-iterations`: максимальное число итераций execution loop по умолчанию (по умолчанию `5`).

## Backend r1s

Включается флагом `--runtime r1s`; NixOS-настройки описаны в
[модуле agentd](../../modules/agentd/README.md). Требуются `runtime.image`, source repository
и source revision в `TaskSpec`. Cluster key r1s передаётся отдельным runtime-файлом через
`--r1s-cluster-key-file`, без использования Meshbus realm key как неявного fallback.

`--r1s-bootstrap-destinations` и `--r1s-tunnel-peers` принимают списки через запятую;
`--r1s-resource-class`, `--r1s-offer-wait` и `--acp-port` задают параметры workload/tunnel.
`--agent-environment-file` читает runtime JSON string map со scoped environment для агента;
поля source/ACP задаются самим runtime и не переопределяются этим файлом. Control-plane
ключи не передаются контейнеру. Дополнительный host TCP listener не создаётся.

Переназначение execution создаёт новую ACP session с task context. Автоматического replay turn
при обычном обрыве транспорта нет. Отмена request прекращает logical run и освобождает tunnel/client;
allocator-side lease остаётся последней границей жизни execution при недоставленной отмене.

## Проверка

```sh
go test -race ./...
go vet ./...
```

[Live test](./internal/runtime/r1s_live_test.go) запускается отдельно на Linux allocator host:

```sh
R1S_LIVE_IMAGE='<immutable OCI reference>' \
R1S_LIVE_CLUSTER='<joined cluster ID>' \
R1S_LIVE_BOOTSTRAP='<bootstrap path inside image>' \
go test ./internal/runtime -run '^TestR1SContainerTurn$' -v
```

Тест использует реальный Pi/hydra image с локальным LLM fixture и source repo внутри disposable
контейнера. Доступ к joined cluster credentials нужен только тестовому процессу.
