# Embedding runtime

`lattice.embedding-runtime` запускает offline CPU-инференс EmbeddingGemma 2.
Сервис доступен только на loopback; публичные OpenAI и Google API обслуживает
[llm-gateway](./../llm-gateway/README.md). Новый ingress или firewall-порт не нужен.

```nix
lattice.embedding-runtime = {
  enable = true;
  idleSeconds = 1200;
  requestTimeoutSeconds = 300;
  threads = 2;
  memoryMax = "6G";
};
```

Параметры `package` и `modelPath` позволяют выбрать воспроизводимую сборку runtime
и локальный snapshot модели. Стандартные веса закреплены ревизией и SHA-256 каждого
файла в [weights.nix](./../../packages/embedding-runtime/weights.nix); версия Python
и библиотек задаётся корневым [flake.lock](./../../flake.lock). Downloads, pip и
компиляция при запуске сервиса отсутствуют.

Основной HTTP-процесс не импортирует PyTorch. По первому запросу создаётся отдельный
worker с нужными энкодерами. Профиль расширяется только между запросами, после
завершения прежнего процесса, без двух одновременно загруженных копий модели.
Через 20 минут без активных и ожидающих запросов worker завершается. Healthcheck,
каталог и метрики не загружают веса и не продлевают срок хранения модели в RAM.

Инференс последовательный, batch вычислений равен одному элементу. Четыре запроса
могут ожидать; переполнение возвращает `429` с `Retry-After`. Deadline включает
очередь, загрузку и инференс. Disconnect клиента удаляет ожидающий запрос или
останавливает активный worker. Следующий запрос восстанавливает процесс автоматически.
Временные медиа удаляются также после принудительной остановки worker.

Сервис работает от DynamicUser, с ограничениями CPU и RAM, без доступа к домашним
каталогам и внешней сети. Веса читаются из Nix store; `HF_HUB_OFFLINE=1`. Используется
FP32: FP16 для этой модели недопустим. Размерности и порядок частей описаны в
[контракте runtime](./../../packages/embedding-runtime/README.md).

При включённом observability-стеке Prometheus собирает метрики runtime, а Alloy
отправляет JSON-события жизненного цикла в Loki с `service="embedding-runtime"`.
События содержат только код ошибки, профиль и тип события; содержимое запросов,
base64 и векторы не логируются.

Перед включением на новой машине измерьте cold/warm latency и peak RSS для всех
профилей, проверьте реальный мультимодальный инференс и выгрузку процесса:

```sh
embedding-runtime-benchmark --model-path /nix/store/<pinned-model> \
  --output /tmp/embedding-benchmark.json
```

Процедура использует тот же путь idle-выгрузки с ускоренным отсчётом. Unit-тесты и
изолированный Nix module/runtime smoke не требуют весов; отдельная реальная приёмка
описана в [пакете](./../../packages/embedding-runtime/README.md).
