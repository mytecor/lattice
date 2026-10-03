# Выполнение задач через Git, r1s и ACP

Этот документ фиксирует целевую архитектуру постановки и выполнения задач в Lattice. Он заменяет
прежнюю модель F10/F11 с общим controller, собственной очередью, leases, worker registry и
heartbeat. План реализации разложен по [F10](./roadmap/f10-disposable-worker/README.md),
[F11](./roadmap/f11-git-task-pipeline/README.md), [F19](./roadmap/f19-agent-execution-loop/README.md),
[F20](./roadmap/f20-planning-loop/README.md) и [F21](./roadmap/f21-multi-agent/README.md).

## Целевая модель

```text
ACP planning agent
        │ creates task.md
        ▼
       Git
        │
        ▼
   git-watchd
        │
        ▼
      taskd
        │
        ▼
     agentd
        │
        ▼
       r1s
        │
        ▼
 agent container
        │ ACP
        ▼
  execution loop
        │
        ▼
   TaskResult
        │
        ▼
      taskd
        │
        ▼
       Git
```

Главный инвариант:

```text
Git        = durable task/result state
meshbus    = service communication
r1s        = distributed execution
git-watchd = Git revision changes
taskd      = task reconciliation
agentd     = agent execution
ACP        = agent communication
```

Ни один слой не повторяет ответственность соседнего. Все сервисы, кроме `r1s` и `meshbus`,
размещаются в этом репозитории.

## Источники состояния

Git — единственный durable source of truth для постановки задачи и terminal result. В Git нельзя
хранить runtime state: `running`, `worker`, `execution_id`, `attempt`, lease, heartbeat, ACP
session и подобные поля. Конкретная версия постановки идентифицируется тройкой:

```text
task repository + path + Git commit
```

Изменение `task.md` создаёт новую immutable revision постановки. До появления terminal `result`
revision считается actionable.

ACP session и конкретный r1s execution — ephemeral state: они могут исчезнуть без процедуры
восстановления. `taskd` и `agentd` также могут потерять всё локальное runtime state. После сбоя
незавершённая работа обнаруживается повторным чтением Git, а workload возобновляется возможностями
logical run в r1s или запускается повторно.

Система обязана быть корректной при повторном выполнении одной task revision. Git-коммиты служат
checkpoint; cache, logs, metrics и локальные workspace не являются источниками истины.

## Контракт `task.md`

Задача целиком хранится в Markdown с YAML front matter. Минимальная постановка:

```yaml
---
id: task-id

source:
  repo: ...
  revision: ...

runtime:
  image: ...
  max_attempts: 5
---

# Задача

## Context

...

## Requirements

...

## Acceptance criteria

...
```

Опциональные проверки задаются декларативно:

```yaml
checks:
  - go test ./...
  - nix flake check
```

Для execution loop дополнительно допускаются явные ограничения `max_iterations` и `timeout`.
`max_attempts` ограничивает число полных запусков task revision; это входная policy, а не
сохраняемый счётчик текущего attempt.

Terminal result записывается обратно в тот же task:

```yaml
result:
  status: completed
  revision: ...
```

и при необходимости дополняется секциями:

```markdown
## Result

...

## Verification

...
```

Минимальный набор terminal states: `completed`, `failed`, `blocked`. Поля `state: running`,
`worker`, `execution`, `attempt` и их аналоги запрещены: они смешивают durable task contract с
эфемерным состоянием исполнения.

## Границы сервисов

### `git-watchd`

`git-watchd` — generic adapter `repo/ref → Git revision changes`. Он ничего не знает про
`tasks/*.md`, `TaskSpec`, ACP, `agentd` или r1s.

Сервис предоставляет через meshbus два запроса:

- `git.head { repo, ref } → { revision }`;
- `git.watch { repo, ref, after_revision } → { from, to, changed_paths[] }`.

`git.watch` работает как long poll. По server-side timeout он возвращает `no_change`, после чего
consumer вызывает его снова. Git уже является event log, поэтому отдельные event database, ack,
replay queue, consumer offsets и exactly-once delivery не вводятся; cursor равен Git revision.

### `taskd`

`taskd` — reconciler между Git и execution layer:

```text
Git → actionable tasks → TaskSpec → agentd → TaskResult → Git
```

При старте `taskd` получает текущий head, сканирует task repository, находит все actionable
revision, reconciles их и только затем начинает `git.watch` от полученного head. Correctness не
зависит от сохранённого cursor.

Допускается только теряемый runtime mapping вида `task revision → currently running request`.
`taskd` не знает r1s execution ID, allocator, ACP session, agent iteration или tunnel. Его граница
с `agentd` — только `TaskSpec → TaskResult`.

### `agentd`

`agentd` выполняет `TaskSpec` и возвращает `TaskResult`. Внутри одного сервиса код делится на
components `runtime`, `acp`, `loop` и `verifier`; отдельными сервисами они пока не становятся.

```text
RunTask(TaskSpec)
        │
        ▼
start logical r1s run
        │
        ▼
agent OCI container
        │
        ▼
connect ACP through r1s tunnel
        │
        ▼
create ACP session and send task
        │
        ▼
run configured verification
   │                       │
 success                 failure
   │                       │
   │              ACP feedback and continue
   ▼
TaskResult
```

`agentd` использует public library API r1s, когда он предоставляет нужную возможность, и не
shell-out'ится в CLI без необходимости. Lattice не реализует поверх r1s scheduler, allocator,
worker registry, heartbeat или собственные execution leases.

### Agent runtime

Контейнер — disposable, но внутри конкретного r1s execution работает long-lived ACP endpoint:

```text
agent container
├── workspace
├── git
├── Pi
├── pi-acp
└── ACP listener
```

При старте он получает source repository, source revision, task context и ограниченные
credentials/capabilities, после чего создаёт workspace. Уникальное состояние задачи не живёт
только внутри контейнера.

`agentd` открывает r1s tunnel к ACP-порту контейнера и действует как обычный ACP client. ACP —
единственный agent protocol; отдельный Lattice-specific RPC для Pi не вводится.

### `r1s` и `meshbus`

`r1s` считается готовым execution fabric и отвечает за OCI workload execution, placement и
переназначение logical run. Lattice использует его возможности, а не дублирует их.

`meshbus` — транспорт между сервисами. Сервисы находят друг друга через subjects и wildcard
subscriptions. Отдельные service registry, service-discovery metadata и HTTP API между внутренними
сервисами не вводятся.

## Recovery

При потере конкретного execution r1s переназначает logical run. `agentd` подключается к новому
ACP endpoint, создаёт новую session и восстанавливает контекст из task specification, текущего
Git state, предыдущих commits и последнего verifier result. Восстанавливать старую ACP session не
требуется.

При потере самого `agentd` его session и runtime mapping можно потерять. После восстановления
`taskd` снова увидит revision без terminal result и запустит её. При потере `taskd` полный startup
scan Git восстанавливает actionable set.

## Первая реализация

Сначала весь путь доказывается детерминированным dummy workload:

```text
task.md → git-watchd → taskd → agentd → r1s → dummy workload → TaskResult → Git
```

Dummy workload клонирует repository, пишет детерминированный файл, делает commit и возвращает
result revision. Acceptance включает restart `git-watchd`, `taskd` и `agentd`, потерю r1s
execution, повторную обработку одной task revision, отсутствие потерянных задач и независимость
correctness от runtime state сервисов. Только после этого подключается Pi/ACP execution loop.

Первая версия verifier запускает настроенные acceptance commands. Успех создаёт
`TaskResult(completed)`, ошибка возвращается агенту как ACP feedback в той же session. Цикл
ограничен `max_iterations`, `max_attempts` и `timeout`; отдельный planner/reviewer loop не входит в
первую версию.

## Planning и multi-agent

Planning agent использует существующий ACP ingress Lattice. Его единственный durable output — Git
commit с `task.md`; напрямую `agentd` или r1s он не вызывает.

Multi-agent остаётся отдельным поздним этапом. Root agent сможет запускать child workloads через
r1s и общаться с ними по ACP через r1s tunnels. Ephemeral child живёт не дольше владеющего root
process/run; `parent_id` не добавляется в control state. Работа, которая должна пережить root
agent, оформляется новой durable `task.md`.

## Явно вне архитектуры

Не создаются controller database, task queue, worker registry, scheduler, Lattice execution
leases, heartbeat protocol, generic event journal, service registry, metadata discovery в
meshbus, внутренний HTTP API и отдельный `eventd`. Отдельного `workd` и общего `controller` также
нет: их прежние обязанности разделены между `taskd`, `agentd`, r1s и Git.

## Definition of done вертикали

Полный milestone готов, когда человек или planning agent commit'ит `task.md`; `git-watchd`
обнаруживает revision; `taskd` создаёт `TaskSpec`; `agentd` запускает disposable agent через r1s,
подключается по ACP через tunnel и ведёт verification loop; агент изменяет source repository;
`TaskResult` возвращается в `taskd` и записывается как terminal result в Git; restart любого
Lattice-сервиса не требует иных durable данных, кроме Git и возможностей r1s.
