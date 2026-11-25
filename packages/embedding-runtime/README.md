# EmbeddingGemma 2 runtime

Нативный Nix-пакет сервиса и disposable CPU worker. NixOS wiring находится в
[модуле](./../../modules/embedding-runtime/README.md). Сборка:

```sh
nix build .#embedding-runtime
nix build .#embedding-runtime.weights
```

Веса `google/embeddinggemma-2` зафиксированы на
`f9972ac22e696a105c482202646979e33e4b5a1e`. Изменение ревизии, препроцессинга или
инструкций требует явного решения о совместимости ранее построенных индексов.
Fallback на другую embedding-модель отсутствует.

## Внутренний HTTP-контракт

* `GET /healthz`: состояние HTTP supervisor.
* `GET /models`: статический каталог возможностей, без загрузки модели.
* `GET /metrics`: состояние worker/энкодеров, очередь, загрузки/выгрузки,
  длительности и ошибки в Prometheus text format.
* `POST /embed`: общий типизированный контракт
  [llm-gateway](https://github.com/mytecor/llm-gateway/blob/main/docs/embeddings.md).

```json
{
  "model": "google/embeddinggemma-2",
  "items": [{
    "parts": [{"text": "Search for a bicycle"}],
    "task_type": "RETRIEVAL_QUERY",
    "dimensions": 768
  }]
}
```

Альтернатива `parts` — `token_ids` с точными native ID токенизатора модели, без
decode/re-tokenize. Ответ содержит `embeddings` в исходном порядке; каждый элемент —
`values` и фактическое число обработанных `tokens`. Публичный gateway переводит его
в OpenAI или Google response и возвращает логическую модель `embedding`.

Один элемент может смешивать текст, JPEG/PNG/WebP, WAV/MP3/FLAC/OGG и MP4/WebM в
упорядоченных `parts`. Медиа передаются через `inlineData.mimeType/data` (base64).
URL, пути файлов, PDF и Files API не поддерживаются. Literal placeholders модели
в тексте отклоняются, чтобы сохранить однозначное положение медиа.

Размерности — 768, 512, 256, 128, с L2-нормализацией после усечения. Лимит одного
элемента — 8192 токена с учётом медиа и task prefix; overflow отклоняется без скрытого
усечения текста или аудио. До 64 элементов в запросе. Публичный JSON ограничен
32 MiB; внутренний canonical/NDJSON envelope — 40 MiB.

Изображения ограничены 16 мегапикселями, audio/video — 330 секундами независимо
от контекста. Аудио приводится к mono 16 kHz, видео — к 1 fps с максимум 32
равномерно выбранными кадрами. Аудиодорожка видео автоматически не включается.

Task types и title применяются только к тексту. Без task type префикс отсутствует;
`title` разрешён только для `RETRIEVAL_DOCUMENT`.

## Проверки

Локальная проверка лёгкого supervisor без установки ML-стека:

```sh
uv run --no-project --with aiohttp python -m unittest discover -s tests -v
```

Для реальной приёмки запускайте тесты из checkout в окружении Nix-пакета, указав
`EMBEDDING_MODEL_PATH` с закреплёнными весами. Они сравнивают результаты с прямым
SentenceTransformer, проверяют native token IDs, размерности, повторяющиеся
interleaved images и отсутствие обрезки audio после 30 секунд. Обычная CI-проверка
эти тесты пропускает; тестовый worker не выдаётся за настоящий инференс.

`embedding-runtime-benchmark` проверяет все заявленные MIME-типы, четыре профиля,
детерминизм, влияние изображения на вектор и фактическое завершение worker после
idle expiry. Замеры выводятся без payload и векторов.

## Замеры на homelab

[Результаты приёмки N100](./benchmark-homelab.json), 2026-11-17: 16 ГБ RAM,
FP32, два потока. Короткие синтетические входы, время зависит от содержимого:

| Профиль | Cold, с | Warm, с | Peak RSS worker, GiB |
| --- | ---: | ---: | ---: |
| Текст | 8,58 | 0,18 | 2,07 |
| Vision | 26,90 | 18,36 | 3,01 |
| Audio | 9,79 | 0,80 | 3,78 |
| Полный смешанный вход | 45,31 | 34,94 | 4,72 |

Peak RSS не включает supervisor и временные decoder-процессы. Проверены все
заявленные MIME-типы и фактическое завершение worker. На ноде задан `MemoryMax=6G`.

Обычные OpenAI и Google GenAI SDK проверяются отдельным
[скриптом совместимости](./../../scripts/check-embedding-clients.py). Он проверяет
совпадение текстовых векторов, native token IDs, каталог модальностей и реальное
изображение, сериализованное Google SDK (включая URL-safe base64). Ключ передаётся
через stdin и не выводится:

```sh
cat /path/to/client-key | uv run --no-project \
  --with openai --with google-genai --with pillow --with numpy \
  python scripts/check-embedding-clients.py --base-url https://gateway.example
```
