# Перевести shared RNS daemon на Reticulum-Go

## Контекст

Нода использует `rns-rs` как общий Reticulum shared instance для `rnsh`, `r1s` и `r1sd`.
Доставка HEADER_1 `LINKREQUEST`/`DATA` между sibling-клиентами зависит от локального патча
[`shared-local-delivery.patch`](../../packages/rns-rs/shared-local-delivery.patch), которого нет в
закреплённом upstream `rns-rs`.

`r1s` и `meshbus` уже используют Reticulum-Go v1.2.0 как клиентский стек. Переход daemon на ту же
реализацию убирает Rust/Go shared-instance boundary и позволяет удалить локальный server-патч.
Эта миграция не решает bootstrap allocator'а в `r1s`: детерминированное получение известных
destination'ов остаётся отдельной upstream-задачей.

## Что сделать

- [ ] Закрепить и собрать Reticulum-Go v1.2.0 отдельным Nix-пакетом; не использовать отстающую
  версию из текущего `nixpkgs`.
- [ ] Перевести shared-instance systemd unit на `reticulum-go`, сохранив типизированный контракт
  конфигурации: transport mode, Unix shared instance `@rns/default`, публичные TCP uplink'и,
  identity/state persistence и закрытый наружу control plane.
- [ ] Сделать зависимость [`profiles/rnsh`](../../profiles/rnsh/config.nix) от daemon нейтральной:
  убрать жёсткую проверку TCP control port старого `rns-server` и проверять готовность фактического
  Unix data/RPC endpoint.
- [ ] Переключить `worker-runtime.rnsInstanceService` на новый unit без изменения fail-closed
  поведения `r1s`/`r1sd`.
- [ ] Оставить upstream Rust `rnsh`, но удалить Rust server package и
  [`shared-local-delivery.patch`](../../packages/rns-rs/shared-local-delivery.patch) после
  успешного cutover.
- [ ] Обновить архитектуру, эксплуатационную документацию и observability service lists.
- [ ] Сохранить предыдущую NixOS generation как проверенный rollback до завершения live-приёмки.

## Что проверить отдельно

- [ ] На изолированных временных сокетах два Reticulum-Go shared client проходят sibling announce,
  known-destination PathRequest, Link, Channel и Resource через полный daemon-процесс.
- [ ] `r1s run` и `r1sd` подключаются к `@rns/default`, а отсутствие daemon по-прежнему приводит к
  fail-closed ошибке, но не к запуску частного shared instance.
- [ ] Rust `rnsh` listener работает через Go daemon; Python `rnsh` с операторской машины выполняет
  авторизованную команду и отклоняет неизвестную identity.
- [ ] Отдельно проверены оба публичных uplink — Sydney и ReticulumNet — без LAN discovery.
- [ ] После рестарта daemon сохраняются transport identity и достижимость прежнего `rnsh`
  destination; после рестарта `worker-runtime` сохраняется allocator identity.
- [ ] Одновременные `rnsh`, `r1s` и `r1sd` не создают гонок shared-instance записи и не теряют
  Link/Channel сообщения под `go test -race`/live concurrency smoke.
- [ ] Предупреждения `invalid announce signature` и path-request diagnostics коррелируются с
  destination hash тестового трафика, а не оцениваются только по наличию строки в журнале.

## Критерий готовности (Definition of Done)

- [ ] Нода работает на Reticulum-Go v1.2.0 без локального патча `rns-rs`; `rnsh`, `r1s` и `r1sd`
  проходят перечисленную live-приёмку через общий shared instance.
- [ ] `nix flake check --all-systems --no-build`, сборки затронутых x86_64-linux checks и проверка
  ссылок проходят; процедура rollback проверена до удаления старого server package.

## Затрагиваемые файлы / слои

- [`packages`](../../packages/README.md) — пакет Reticulum-Go и удаление server-патча.
- [`modules/rns-server`](../../modules/rns-server/README.md) — backend shared daemon.
- [`profiles/rns-network`](../../profiles/rns-network/README.md) — публичные uplink'и.
- [`profiles/rnsh`](../../profiles/rnsh/config.nix) — нейтральная зависимость и readiness.
- [`modules/worker-runtime`](../../modules/worker-runtime/README.md) — имя shared-instance unit.
- [`nodes/mytecor-homelab`](../../nodes/mytecor-homelab/README.md) — staged cutover и live-приёмка.
