# План по фичам

Второй уровень роадмапа: конкретные фичи, из которых складывается сеть. Каждая фича — это
самостоятельная вертикаль с задачами и критерием готовности. Верхнеуровневая картина — в
[VISION.md](../VISION.md), отдельные задачи — в [`tasks/`](../tasks/README.md).

## Как читать

- Номер фичи — стабильный идентификатор. Фактический порядок задаётся явными зависимостями и
  основным маршрутом в `VISION.md`.
- У каждой фичи отдельный файл в этой папке.
- Новая фича заводится по [TEMPLATE.md](./TEMPLATE.md).
- Каждая фича ссылается на свои задачи в [`tasks/`](../tasks/README.md).
- Незакрытые вопросы ждут в [`BACKLOG.md`](../BACKLOG.md).
- Известные расхождения документации с кодом — в [DIVERGENCES.md](./DIVERGENCES.md).

## Индекс фич

| Фича | Файл | Что это |
| ----- | ---- | ------- |
| **F1. Одна железная нода в работе** | [f1-one-node.md](./f1-one-node.md) | первый узел с нуля, переживает перезагрузку, сам применяет коммит |
| **F2. Секреты и идентичность узла** | [f2-secrets-identity.md](./f2-secrets-identity.md) | безопасность на ключах, публикуемый репозиторий |
| **F3. Reticulum поверх TCP/IP** | [f3-reticulum-tcp.md](./f3-reticulum-tcp.md) | связь по интернету и удалённый доступ по RNS-адресу |
| **F4. Полезная нагрузка** | [f4-payload.md](./f4-payload.md) | сервисы, реплика кода, вычисления и хранилище |
| **F5. Внешние узлы** | [f5-external-nodes.md](./f5-external-nodes.md) | чистая граница узла, внешний репозиторий |
| **F6. Радио и mesh** | [f6-radio-mesh.md](./f6-radio-mesh.md) | LoRa/RNode как интерфейс Reticulum |
| **F7. LLM gateway** | [f7-llm-gateway.md](./f7-llm-gateway.md) | логические модели, routing и изоляция provider credentials |
| **F8. Интерактивный Pi runtime** | [f8-pi-runtime.md](./f8-pi-runtime.md) | один harness для TUI сейчас и RPC workers позже |
| **F9. Cache и artifact plane** | [f9-cache-artifact-plane.md](./f9-cache-artifact-plane.md) | Git/npm/Nix caches и отдельное хранение результатов |
| **F10. Disposable worker** | [f10-disposable-worker.md](./f10-disposable-worker.md) | одноразовое выполнение задачи через Pi RPC |
| **F11. Controller** | [f11-controller.md](./f11-controller.md) | очередь, leases, worker registry и состояние задач |
