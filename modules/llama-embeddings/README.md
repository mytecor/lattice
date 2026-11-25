# EmbeddingGemma 2 в llama-server

Lattice закрепляет llama.cpp в [flake.lock](./../../flake.lock) и пару
BF16 GGUF из [ggml-org/embeddinggemma-2-GGUF](https://huggingface.co/ggml-org/embeddinggemma-2-GGUF).
llama-server загружает модель по запросу и освобождает веса после 20 минут
простоя. Для инференса используются локальные файлы Nix store.

## Mac с Apple Silicon

[Установщик](./../../scripts/install-mac-llama-embeddings.sh) собирает пакет
и запускает его через LaunchAgent. В этой сборке llama.cpp использует Metal.

```sh
./scripts/install-mac-llama-embeddings.sh mytecor-homelab.local
curl --fail http://127.0.0.1:9226/v1/models
```

Аргумент SSH включает отдельный LaunchAgent с обратным туннелем. Укажите уже
доверенный SSH-адрес хомлаба с доступом по ключу. Gateway на хомлабе обращается
к `127.0.0.1:9227` через этот туннель. Для локального запуска опустите аргумент.

## NixOS и gateway

`lattice.llama-embeddings.enable = true` запускает CPU-сервис с локальным
HTTP-адресом `127.0.0.1:9226`. [Конфигурация хомлаба](./../../nodes/mytecor-homelab/config.nix)
отдаёт приоритет Mac для модели `embedding-gguf`. Резерв использует
те же BF16 GGUF. Gateway адаптирует свои OpenAI и Google endpoints к
нативному `POST /v1/embeddings` llama-server, передаёт inline-медиа в порядке
получения и поддерживает 128, 256, 512 и 768 измерений.

## Совместимость векторов

На выборке из четырёх JPEG cosine между llama.cpp и прежним runtime оказался
в диапазоне 0,928–0,972. При поиске по шести текстовым запросам на этой
выборке во всех шести случаях изменился первый результат. Для этих же кадров
cosine между Metal и CPU с одинаковой парой GGUF был не ниже 0,99999
на трёх проверенных фото. Рабочий индекс использует прежнюю модель `embedding`.
