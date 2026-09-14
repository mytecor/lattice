# F10. Disposable worker

Одна задача выполняется в чистом одноразовом окружении: worker получает спецификацию, клонирует
репозиторий, создаёт environment, запускает Pi RPC, публикует результат и уничтожается. Ни checkout,
ни dependency directories, ни Pi session не являются состоянием продолжения задачи.

До готовности r1s локальный execution path строится непосредственно на `containerd` и тонком
`LocalExecutor`, без временного scheduler/allocator. Интерактивный оркестратор и disposable workers
используют один Pi runtime и один OCI image: различаются task context, workspace, разрешения и
политика persistence, но не Pi package, model config, tools или extensions.

Зависит от [F8](../f8-pi-runtime/README.md) и [F9](../f9-cache-artifact-plane/README.md). Соответствует
[вехе 10](../../ROADMAP.md#f10-disposable-worker).

Задачи: [f10-01](./f10-01-task-specification.md),
[f10-02](./f10-02-worker-isolation.md),
[f10-03](./f10-03-worker-lifecycle.md),
[f10-04](./f10-04-pi-rpc-runner.md),
[f10-05](./f10-05-worker-credentials.md),
[f10-06](./f10-06-disposability-acceptance.md).

**Критерий готовности:** вручную запущенный worker выполняет задачу от чистого старта до
commit/push/result, после уничтожения запускается заново и продолжает только из repo state, task
specification и явно сохранённых artifacts. На worker нет уникального состояния или постоянных
provider credentials.

**Осознанно откладываем (до F11):** очередь, leases, автоматический provisioning и retry задач.

## Зафиксированная архитектура

```text
ACP client
    ↓
hydra-acp → pi-acp                         host ingress/session plane
                ↓ PI_ACP_PI_COMMAND
        orchestrator Pi container
                ↓ pi-subagents external-job
        /run/lattice/worker.sock
                ↓
        LocalExecutor → containerd
                ↓
        disposable Pi container
```

- [f10-02](./f10-02-worker-isolation.md) фиксирует `containerd`, общий immutable OCI image и
  декларативные worker classes вместо деклараций отдельных runtime-контейнеров.
- [f10-03](./f10-03-worker-lifecycle.md) фиксирует узкий local broker/executor, который позднее
  заменяется r1s-adapter без протекания allocation protocol в Lattice.
- [f10-04](./f10-04-pi-rpc-runner.md) фиксирует host-side `pi-acp`, контейнерный Pi через
  `PI_ACP_PI_COMMAND` и интеграцию с `pi-subagents` через внешний job provider.
