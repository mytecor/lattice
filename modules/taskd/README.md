# Модуль `lattice.taskd`

NixOS-модуль для запуска демона Git task reconciler `taskd`.

`taskd` отслеживает изменения репозитория задач через `git-watchd`, транслирует
actionable задачи в `TaskSpec` и вызывает `agentd` через Meshbus.

Подробнее об архитектуре: [TASK_EXECUTION.md](../../TASK_EXECUTION.md) и
[f11-03-taskd.md](../../roadmap/f11-git-task-pipeline/f11-03-taskd.md).

## Пример конфигурации

```nix
{
  lattice.taskd = {
    enable = true;
    realmKeyFile = config.age.secrets.meshbus-realm-key.path;
    repoPath = "/var/lib/tasks";
    ref = "HEAD";
  };
}
```
