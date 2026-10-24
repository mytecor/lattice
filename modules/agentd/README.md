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

## Disposable ACP backend (f19-02)

```nix
lattice.agentd = {
  enable = true;
  realmKeyFile = "/run/agenix/meshbus-realm";
  runtime = "r1s";
  r1s = {
    clusterKeyFile = "/run/agenix/r1s-cluster-key";
    bootstrapDestinations = [ "<public allocator destination hash>" ];
    # Optional runtime JSON string map with scoped agent environment:
    # agentEnvironmentFile = "/run/agenix/agent-environment";
  };
};
```

Cluster key — 32 raw bytes или 64 hex characters, отдельный от Meshbus realm key.
Он и optional environment file загружаются через `LoadCredential`; значения не читаются Nix
и не помещаются в store или argv. Контейнер получает только agent environment и source/ACP inputs.
Настройки `tunnelPeers`, `resourceClass`, `acpPort`, `offerWait` управляют run/tunnel без
публикации backend-порта в firewall.

По умолчанию остаётся `runtime = "dummy"`. Backend `r1s` реализует ACP turn и замену session
при переназначении execution, но удалённая верификация и source result collection остаются в
[f19-03](../../roadmap/f19-agent-execution-loop/f19-03-verification-recovery.md).
До её реализации после turn возвращается явная ошибка верификации; checks не исполняются на хосте.
