# F10. Disposable worker

Одна задача выполняется в чистом одноразовом окружении: worker получает спецификацию, клонирует
репозиторий, создаёт environment, запускает Pi RPC, публикует результат и уничтожается. Ни checkout,
ни dependency directories, ни Pi session не являются состоянием продолжения задачи.

Execution path строится поверх [r1s](https://github.com/mytecor/r1s) — готового децентрализованного
OCI workload fabric поверх Reticulum. `r1sd`-allocator выполняет OCI workload через `containerd`;
Lattice не строит собственный scheduler/allocator и не протаскивает allocation protocol внутрь.
Ранее планировавшийся тонкий временный `LocalExecutor` как замена r1s больше не нужен: r1s готов и
становится execution backend. Интерактивный оркестратор и disposable workers используют один Pi
runtime и один OCI image: различаются task context, workspace, разрешения и политика persistence, но
не Pi package, model config, tools или extensions.

Зависит от [F8](../f8-pi-runtime/README.md) и [F9](../f9-cache-artifact-plane/README.md). Соответствует
[вехе 10](../../ROADMAP.md#f10-disposable-worker).

Задачи: [f10-01](./f10-01-package-r1s.md),
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
        r1s request → r1sd allocator → containerd
                ↓
        disposable Pi container
```

- [f10-01](./f10-01-package-r1s.md) фиксирует r1s как flake-пакет (клиент `r1s` + allocator
  `r1sd`) с го-тулчейном 1.27.1 из основного пина nixpkgs; закрыто 2026-09-16.
- [f10-04](./f10-04-pi-rpc-runner.md) фиксирует host-side `pi-acp`, контейнерный Pi через
  `PI_ACP_PI_COMMAND` и интеграцию с `pi-subagents` через внешний job provider.
