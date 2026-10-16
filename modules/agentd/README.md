# Модуль `lattice.agentd`

NixOS-модуль для запуска демона исполнения задач `agentd`.

`agentd` слушает шину Meshbus по теме `agent.run`, принимает спецификации задач `TaskSpec`,
запускает их исполнение через OCI/dummy workload и возвращает terminal `TaskResult`.

Подробнее об архитектуре: [TASK_EXECUTION.md](../../TASK_EXECUTION.md) и
[f11-04-dummy-pipeline.md](../../roadmap/f11-git-task-pipeline/f11-04-dummy-pipeline.md).

## Пример конфигурации

```nix
{
  lattice.agentd = {
    enable = true;
    realmKeyFile = config.age.secrets.meshbus-realm-key.path;
  };
}
```
