# F10. Disposable worker

Одна задача выполняется в чистом одноразовом окружении: worker получает спецификацию, клонирует
репозиторий, создаёт environment, запускает Pi RPC, публикует результат и уничтожается. Ни checkout,
ни dependency directories, ни Pi session не являются состоянием продолжения задачи.

Зависит от [F8](../f8-pi-runtime/README.md) и [F9](../f9-cache-artifact-plane/README.md). Соответствует
[вехе 10](../VISION.md#вехи-и-зависимости-без-деталей).

Задачи: [f10-01](f10-01-task-specification.md),
[f10-02](f10-02-worker-isolation.md),
[f10-03](f10-03-worker-lifecycle.md),
[f10-04](f10-04-pi-rpc-runner.md),
[f10-05](f10-05-worker-credentials.md),
[f10-06](f10-06-disposability-acceptance.md).

**Критерий готовности:** вручную запущенный worker выполняет задачу от чистого старта до
commit/push/result, после уничтожения запускается заново и продолжает только из repo state, task
specification и явно сохранённых artifacts. На worker нет уникального состояния или постоянных
provider credentials.

**Осознанно откладываем (до F11):** очередь, leases, автоматический provisioning и retry задач.
