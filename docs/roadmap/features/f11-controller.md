# F11. Controller

Controller автоматизирует жизненный цикл disposable workers и хранит единственное ценное
операционное состояние вычислительного контура: очередь, leases, task state и worker registry.
Source и task definitions остаются в Git, artifacts — в object storage.

Зависит от [F10](./f10-disposable-worker.md). Соответствует
[вехе 11](../VISION.md#вехи-и-зависимости-без-деталей).

Задачи: [f11-01](../tasks/f11-01-control-state-model.md),
[f11-02](../tasks/f11-02-queue-leases-registry.md),
[f11-03](../tasks/f11-03-worker-provisioner.md),
[f11-04](../tasks/f11-04-idempotent-recovery.md),
[f11-05](../tasks/f11-05-result-publication.md),
[f11-06](../tasks/f11-06-end-to-end-recovery.md).

**Критерий готовности:** controller принимает task specification, выдаёт lease одноразовому
worker, получает commit/result/artifact references и завершает задачу; перезапуск controller или
потеря worker не теряет задачу и не приводит к двойной публикации результата.

**Осознанно откладываем:** многохостовый scheduler и autoscaling до появления измеренной нагрузки.
