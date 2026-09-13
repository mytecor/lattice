# Реестр задач

Третий уровень роадмапа. Задачи сгруппированы по фичам и живут в каталоге своей фичи
(`<feature-id>-<feature-slug>/`): каждая задача — отдельный файл
`<feature-id>-<task-id>-<task-slug>.md` рядом с файлом самой фичи (`README.md`).
Верхнеуровневая картина — в [VISION.md](../VISION.md), план по фичам — в
[`features/README.md`](../features/README.md).

## Как завести новую задачу

1. Откройте каталог нужной фичи (например `../f7-llm-gateway/`) и скопируйте
   [TEMPLATE.md](../tasks/TEMPLATE.md) в `<feature-id>-<task-id>-<task-slug>.md`.
2. Заполните разделы, закройте чек-боксы по мере работы.
3. Добавьте ссылку на задачу в файл своей фичи (`README.md` в каталоге `<feature-id>-<feature-slug>/`).
4. Незакрытые вопросы — в [BACKLOG.md](../BACKLOG.md).

## Индекс по фичам

Каждая строка ведёт в каталог фичи, где лежат и сам файл фичи, и её задачи.

| Фича | Каталог |
| ------- | ------- |
| **F1. Одна железная нода в работе** | [`../f1-one-node/`](../f1-one-node/README.md) |
| **F2. Секреты и идентичность узла** | [`../f2-secrets-identity/`](../f2-secrets-identity/README.md) |
| **F3. Reticulum поверх TCP/IP** | [`../f3-reticulum-tcp/`](../f3-reticulum-tcp/README.md) |
| **F4. Полезная нагрузка** | [`../f4-payload/`](../f4-payload/README.md) |
| **F5. Внешние узлы** | [`../f5-external-nodes/`](../f5-external-nodes/README.md) |
| **F6. Радио и mesh** | [`../f6-radio-mesh/`](../f6-radio-mesh/README.md) |
| **F7. LLM gateway** | [`../f7-llm-gateway/`](../f7-llm-gateway/README.md) |
| **F8. Интерактивный Pi runtime** | [`../f8-pi-runtime/`](../f8-pi-runtime/README.md) |
| **F9. Cache и artifact plane** | [`../f9-cache-artifact-plane/`](../f9-cache-artifact-plane/README.md) |
| **F10. Disposable worker** | [`../f10-disposable-worker/`](../f10-disposable-worker/README.md) |
| **F11. Controller** | [`../f11-controller/`](../f11-controller/README.md) |
