# F4. Полезная нагрузка

Узел перестаёт быть только участником сети: несёт первый прикладной сервис и реплику кода,
из которой может обновляться без GitHub. Границы будущих вычислений и хранения определены здесь,
а реализация agent runtime и disposable workers разложена в F7–F11. Соответствует
[вехе 4](../../ROADMAP.md#f4-полезная-нагрузка).

Задачи: [f4-01](f4-01-radicle-seed-comin.md),
[f4-02](f4-02-app-services-profile.md),
[f4-03](f4-03-shared-storage-compute.md),
[f4-04](f4-04-enrich-node-status.md),
[f4-05](f4-05-yggdrasil-public-subdomain-ingress.md).

**Статус:** source bootstrap из f4-01 работает на homelab, репозиторий публично реплицирован и
доступен чистому клиенту. Строгий drill без GitHub и полный bootstrap новой NixOS-ноды ещё не
закрыты. F4-02 и [f4-04](f4-04-enrich-node-status.md) выполнены (2026-09-05 и 2026-09-16): Caddy
публикует node-status endpoint и Radicle HTTP API; `status.<node>.local` отдаёт runtime-метаданные
(generation, применённый commit).

**Критерий готовности:** конфигурация сети распространяется между узлами без GitHub, и на узлах
работает хотя бы один прикладной сервис, доступный через шлюз.

**Не входит:** shared filesystem или worker orchestration. Pi, gateway, caches, workers и controller
реализуются отдельными вертикалями F7–F11.
