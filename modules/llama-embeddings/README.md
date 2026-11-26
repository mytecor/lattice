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

## Gateway

[Конфигурация хомлаба](./../../nodes/mytecor-homelab/config.nix)
балансирует модель `embedding` между Mac и [CPU-Max](./../cpu-max/README.md).
Gateway преобразует OpenAI и Google endpoints в нативный `POST /v1/embeddings`,
передаёт inline-медиа в порядке получения и поддерживает 128, 256, 512 и 768
измерений.

## Совместимость векторов

Голый `llama-server` игнорирует EXIF-ориентацию (`stb_image`) и без ограничения
бюджета токенов использует динамическое разрешение `gemma4v`, что снижало cosine
против CPU-Max и прежнего индекса до 0,916–0,972. Чтобы не поднимать отдельный
прокси на каждой машине с `llama.cpp`, выравнивание изображений выполняется в
`llm-gateway` (`embeddings-driver`) опцией `image_align = "embeddinggemma2"` в
[конфигурации хомлаба](./../../nodes/mytecor-homelab/config.nix): драйвер
применяет EXIF-поворот и `BICUBIC`-масштабирование на официальную сетку
EmbeddingGemma 2 (бюджет 280 soft-токенов, шаг 48 px) в PNG.

С `image_align = "embeddinggemma2"` проверенные значения cosine для одинаковых
изображений:
- `llama-server` (BF16 Metal) vs `CPU-Max` (Q8_0) и прежний HF FP32 runtime:
  **0,99935–0,99972** (включая EXIF orientation 6) при одинаковом числе
  vision-токенов;
- `llama-server` vs индекс MLX: **0,99921–0,99964**;
- тексты (`llama-server` vs `CPU-Max`): **0,99991**.

Прямые запросы к локальному `127.0.0.1:9226` в обход `llm-gateway` идут в голый
`llama-server` без этой предобработки.
