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
      Добавлен VM-тест `tests/verdaccio.nix` (CI, x86_64-linux): cold install через
      npm/pnpm/yarn -> warm install тот же graph -> rm cache -> cold install тот же результат.

## Критерий готовности

- [ ] Три поддерживаемых package managers используют один proxy endpoint.
      Реализовано и покрыто VM-тестом; исполняемое подтверждение — в CI (x86_64-linux).
- [ ] Cold install после удаления cache даёт тот же dependency graph по lockfile.
      Реализовано и покрыто VM-тестом; исполняемое подтверждение — в CI (x86_64-linux).

## Затрагиваемые файлы / слои

- `packages/verdaccio/` — пакет через `buildPnpmCli`.
- `modules/verdaccio/` — NixOS-модуль `lattice.verdaccio`.
- `profiles/cache-plane/` — сервис в cache-plane профиле; порт `verdaccio` в `ports.nix`.
- `tests/verdaccio.nix` — VM-тест proxy-поведения.
- `nodes/mytecor-homelab/config.nix` — включение сервиса на ноде.

## Открытые вопросы

_нет_.

## Заметка по статусу (2026-09-13)

Код, модуль и eval завершены; `nix flake check --all-systems --no-build` проходит.
VM-тест (`tests/verdaccio.nix`) исполняется в CI на x86_64-linux — до зелёного
пропуска CI пункты «Критерий готовности» остаются незакрытыми.
