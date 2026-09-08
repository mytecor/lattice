# План по фичам

Второй уровень роадмапа: конкретные фичи, из которых складывается сеть. Каждая фича — это
самостоятельная вертикаль со своим каталогом `<feature-id>-<feature-slug>/`, где лежат файл фичи
(`README.md`) и её задачи. Верхнеуровневая картина — в [VISION.md](../VISION.md).

## Как читать

- Номер фичи — стабильный идентификатор, а каталог называют по имени файла фичи (`f7-llm-gateway/`).
  Фактический порядок задаётся явными зависимостями и основным маршрутом в `VISION.md`.
- У каждой фичи отдельный каталог в этой папке: `../f1-one-node/`, `../f2-secrets-identity/`, …, `../f11-controller/`.
- Внутри каталога фичи — сам файл фичи (`README.md`) и её задачи (по одной на файл, имена вида
  `<feature-id>-<task-id>-<task-slug>.md`).
- Новая фича заводится по [TEMPLATE.md](./TEMPLATE.md).
- Незакрытые вопросы ждут в [`BACKLOG.md`](../BACKLOG.md).
- Известные расхождения документации с кодом — в [DIVERGENCES.md](./DIVERGENCES.md).

## Индекс фич

| Фича | Каталог / файл | Что это |
| ----- | -------------- | ------- |
| **F1. Одна железная нода в работе** | [`../f1-one-node/README.md`](../f1-one-node/README.md) | первый узел с нуля, переживает перезагрузку, сам применяет коммит |
| **F2. Секреты и идентичность узла** | [`../f2-secrets-identity/README.md`](../f2-secrets-identity/README.md) | безопасность на ключах, публикуемый репозиторий |
| **F3. Reticulum поверх TCP/IP** | [`../f3-reticulum-tcp/README.md`](../f3-reticulum-tcp/README.md) | связь по интернету и удалённый доступ по RNS-адресу |
| **F4. Полезная нагрузка** | [`../f4-payload/README.md`](../f4-payload/README.md) | сервисы, реплика кода, вычисления и хранилище |
| **F5. Внешние узлы** | [`../f5-external-nodes/README.md`](../f5-external-nodes/README.md) | чистая граница узла, внешний репозиторий |
| **F6. Радио и mesh** | [`../f6-radio-mesh/README.md`](../f6-radio-mesh/README.md) | LoRa/RNode как интерфейс Reticulum |
| **F7. LLM gateway** | [`../f7-llm-gateway/README.md`](../f7-llm-gateway/README.md) | логические модели, routing и изоляция provider credentials |
| **F8. Интерактивный Pi runtime** | [`../f8-pi-runtime/README.md`](../f8-pi-runtime/README.md) | один harness для TUI сейчас и RPC workers позже |
| **F9. Cache и artifact plane** | [`../f9-cache-artifact-plane/README.md`](../f9-cache-artifact-plane/README.md) | Git/npm/Nix caches и отдельное хранение результатов |
| **F10. Disposable worker** | [`../f10-disposable-worker/README.md`](../f10-disposable-worker/README.md) | одноразовое выполнение задачи через Pi RPC |
| **F11. Controller** | [`../f11-controller/README.md`](../f11-controller/README.md) | очередь, leases, worker registry и состояние задач |
