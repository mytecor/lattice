# CPU-Max на хомлабе

`lattice.cpu-max` запускает [CPU-Max](https://github.com/Yazeedx0/embeddinggemma-cpu-max)
для EmbeddingGemma 2 с Q8_0 GGUF. [Пакет](./../../packages/cpu-max/package.nix)
фиксирует ревизии C-движка и обеих частей модели. На N100 сервис использует
четыре потока и AVX‑VNNI.

Нативный сервер слушает `127.0.0.1:9228` и принимает `POST /v1/embeddings`.
Для мультимодального входа принимает `content` с частями `text`,
`image_url`, `input_audio` и `input_video`. Медиа передаются inline.
Препроцессор декодирует EXIF и масштабирует фото по сетке EmbeddingGemma 2,
чтобы сохранять близость к векторам действующего индекса. Инференс всех
модальностей выполняет CPU-Max.

Gateway использует CPU-Max для `embedding`. Модель `embedding-gguf` ведёт к
[llama-server на Mac](./../llama-embeddings/README.md) с собственным
представлением медиа. Оба маршрута доступны через один публичный адрес gateway.
