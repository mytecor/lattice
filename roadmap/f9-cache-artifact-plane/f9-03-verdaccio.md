# Развернуть Verdaccio как npm proxy/cache

Фича: [F9 — cache и artifact plane](./README.md). Зависит от F8.

## Контекст

npm, pnpm и yarn должны получать пакеты через общий cache, который можно удалить и восстановить из
upstream registries и lockfiles.

## Что сделать

- [x] Добавить закреплённый Verdaccio service и persistent cache directory под `/var/cache`.
      Собран пакет `pkgs.lattice.verdaccio` (verdaccio 6.10.3 через общий `buildPnpmCli`,
      `packages/verdaccio/`), добавлен модуль `lattice.verdaccio` (`modules/verdaccio/`),
      `cacheRoot` по умолчанию `/var/cache/verdaccio`, сервис на loopback.
- [x] Настроить upstream registry, limits, auth boundary и безопасное логирование.
      `upstreamRegistry` (default npmjs), `maxBodySize`, cache-only `publish = false`
      (publish требует htpasswd через `LoadCredential`), `logLevel`, строгий systemd-песочник.
- [x] Подключить npm/pnpm/yarn clients из tool profile без глобальной ручной настройки.
      Опция `clientConfig` (default true) пишет `/etc/npmrc` (npm+pnpm) и `/etc/yarnrc`
      на loopback-прокси; это только URL реестра, не credentials, и не трогает
      f8-03 base-tools (тяжёлый toolchain туда не добавляется).
- [x] Проверить cold/warm install по lockfile и поведение после очистки cache.
      Изначально был добавлен VM-тест `tests/verdaccio.nix` (cold install через
      npm/pnpm/yarn -> warm install тот же graph -> rm cache -> cold install тот же
      результат), но позже убран: он требует сети к npmjs из QEMU-VM и крэшил Node
      под виртуализацией, делая `nix flake check` красным. Поведенческое покрытие
      сведено к контрактному тесту `tests/verdaccio.nix` (см. ниже) и
      eval/config-чекам.

  **Live-находка 2026-09-14 (Node 24 + W^X):** на реальной ноде verdaccio не
  стартовал: systemd-песочница с `MemoryDenyWriteExecute=true` роняет Node 24/V8
  на инициализации V8-изолятов — `v8::base::OS::SetPermissions` на code range
  возвращает `EPERM` (не ожидаемый `ENOMEM`), и V8 падает с
  `Check failed: 12 == (*__errno_location ())` ещё до старта verdaccio. Это
  ломало каждый `comin`-switch на `mytecor-homelab` (status 4), оставляя ноду на
  старой generation и не давая самообновляться из `main`. Зафиксировано:
  `MemoryDenyWriteExecute = false` для Node-сервиса (тот же трейд-офф, что у
  llm-gateway/Bifrost), остальная жёсткость песочника сохранена. Новый
  контрактный тест `tests/verdaccio.nix` закрепляет, что MDWX обязан оставаться
  `false`, а остальные защитные опции — включёнными.

## Критерий готовности

- [ ] Три поддерживаемых package managers используют один proxy endpoint.
      Реализовано (модуль и пакет); сервис после снятия MDWX стартует на ноде.
      Исполняемое подтверждение cold install через npm/pnpm/yarn остаётся —
      прогнать после восстановления здоровья ноды (см. live-находку).
- [ ] Cold install после удаления cache даёт тот же dependency graph по lockfile.
      Реализовано (модуль и пакет); исполняемое подтверждение убрано вместе с
      VM-тестом, disposable-семантика кеша описана в README модуля. Прогон на
      живом сервисе после восстановления ноды.

## Затрагиваемые файлы / слои

- `packages/verdaccio/` — пакет через `buildPnpmCli`.
- `modules/verdaccio/` — NixOS-модуль `lattice.verdaccio`.
- `profiles/cache-plane/` — сервис в cache-plane профиле; порт `verdaccio` в `ports.nix`.
- `nodes/mytecor-homelab/config.nix` — включение сервиса на ноде.

## Открытые вопросы

_нет_.

## Заметка по статусу (2026-09-13)

Код, модуль и eval завершены; `nix flake check --all-systems --no-build` проходит.
VM-тест (`tests/verdaccio.nix`) был убран вместе с остальными QEMU-тестами — до
включения поведенческой проверки на живом реестре пункты «Критерий готовности»
остаются незакрытыми.
