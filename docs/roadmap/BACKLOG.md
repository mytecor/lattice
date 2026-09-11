# Открытые решения и отложенное

Лента незакрытых вопросов. Сюда переносится незавершённая работа вместо того, чтобы оставаться
в голове. Каждая запись — это открытое решение (ждёт своей фичи) или осознанно отложенная
работа.

## Как устроено

- Запись появляется, когда вопрососознанно откладывается.
- Когда вопрос закрывается, запись переносится в ARCHITECTURE.md / `features/` / задачу и
  удаляется отсюда.
- Решение №1 (модель сборки) закрыто и зафиксировано в [ARCHITECTURE.md](../../ARCHITECTURE.md).
- Инструмент секретов выбран: `agenix`; решение зафиксировано в
  [ARCHITECTURE.md](../../ARCHITECTURE.md#секреты).
- Модель идентичности узла закрыта в [f2-02](f2-secrets-identity/f2-02-node-identity.md) и зафиксирована в
  [ARCHITECTURE.md](../../ARCHITECTURE.md#идентичность-узла).
- Bootstrap Radicle закрыт в [f4-01](f4-payload/f4-01-radicle-seed-comin.md): начальный config
  приходит из installer checkout или GitHub, затем selective seed получает публичную реплику, а
  `comin` читает локальное bare storage первым remote.
- Решение по LLM gateway runtime закрыто: [f7-06](f7-llm-gateway/f7-06-go-lip-gonka-cutover.md) сохранила
  отрицательный результат Go LIP PoC, а целевой собственный Go proxy поверх Bifrost Go API и
  прямой cutover закреплены в [f7-07](f7-llm-gateway/f7-07-bifrost-go-proxy.md).

## Открытые решения

3. **Backend изоляции disposable worker** — VM, microVM или контейнер выбирается в F10 после
   фиксации threat model и требований к NixOS provisioning.
4. **Controller storage и provisioner** — конкретные реализации выбираются в F11 после
   стабилизации task specification и ручного worker lifecycle в F10.

Смысл вычислений и хранилища уточнён в [f4-03](f4-payload/f4-03-shared-storage-compute.md): вычисления
выполняются disposable workers, общей persistent FS у них нет, caches не являются source of truth,
а S3 хранит artifacts и другие естественно объектные результаты.

Точки входа Reticulum выбраны в [F3-02](f3-reticulum-tcp/f3-02-define-entry-points.md): два публичных
peer, общий реестр в flake и исходящие TCP-соединения.

## Отложенное

1. **Резервный оверлей (Yggdrasil/I2P)** — нужен ли как дополнительный интерфейс Reticulum или
   достаточно нескольких TCP-точек входа. Решение после F3.

2. **Reticulum interface discovery / auto-connect** — проверить поддержку в закреплённом
   `rns-rs`, затем использовать публичные peers как bootstrap для обнаружения других соседей.

3. **Stdio shim для Zed поверх no-auth LAN ACP endpoint** — stock-клиент
   [`@hydra-acp/cli`](../../packages/hydra-acp/README.md) несовместим с безаутентичным endpoint
   `ws://acp.<nodename>.local/`: для не-loopback хоста требует credential из `remotes.json`, который
   выдаётся только через `/v1/auth/login`, а daemon без master password отвечает `403`. Caddy host
   при этом переписывает любой path во внутренний `/acp`, так что HTTP API клиента недостижимо в
   принципе (отрицательный результат зафиксирован в
   [f8-06](f8-pi-runtime/f8-06-network-acp-daemon.md#отрицательный-результат-stock-hydra-acp-client-как-local-stdio-shim-для-zed)).
   Открытая работа — auth-задача вместе с LAN boundary: выбрать либо включение Hydra master
   password + пересмотр «host целиком ACP-endpoint», либо собственный минимальный
   stdio→WebSocket shim (форма соединения Ferngeist), который не трогает HTTP API гидры.

   Смежная, но закрытая проблема — разрыв ответов на отдельные чанки из-за per-token `messageId`
   (речь не про Zed): решена на стороне daemon трансформером
   [acp-normalizer](f8-pi-runtime/f8-06-network-acp-daemon.md#трансформер-acp-normalizer-стабильный-messageid-на-логическое-сообщение)
   и включена глобально через `lattice.pi-acp-daemon.defaultTransformers`, см.
   [`packages/acp-normalizer`](../../packages/acp-normalizer/README.md).
