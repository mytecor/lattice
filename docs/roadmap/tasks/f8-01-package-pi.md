# Упаковать и закрепить Pi

Фича: [F8 — интерактивный Pi runtime](../features/f8-pi-runtime.md). Зависит от F7.

## Контекст

Pi должен устанавливаться и обновляться тем же декларативным способом, что и остальная NixOS-нода.

## Что сделать

- [x] Закрепить источник и версию Pi.
- [x] Добавить package/module без пользовательской ручной установки.
- [x] Зафиксировать совместимые runtime dependencies и upgrade procedure.
- [x] Добавить smoke check запуска CLI/TUI.

## Критерий готовности

- [x] Pi появляется после rebuild и запускается на чистом пользовательском профиле.
- [x] Версия и зависимости воспроизводятся из flake lock state.

## Затрагиваемые файлы / слои

- `flake.nix`, `flake.lock`
- `modules/pi/`
- `packages/pi/`
- `packages/pnpm-cli-builder/`

## Открытые вопросы

_нет_.

## Реализация

Завершено 2026-09-08. Имя и версия `@earendil-works/pi-coding-agent` закреплены в
`packages/pi/package.nix`; полный dependency graph и registry integrity зафиксированы рядом в
`pnpm-lock.yaml`. Node.js и pnpm приходят из закреплённого `nixpkgs`.

Общий `packages/pnpm-cli-builder` собирает CLI через `fetchPnpmDeps`/`pnpmConfigHook` offline в
Nix sandbox. Package-specific сборка находится в `packages/pi/package.nix`; `modules/pi` содержит
только enable-опцию и добавляет `pkgs.lattice.pi` в системный профиль. Pi включён непосредственно
в конфигурации homelab; отдельного профиля и mutable pnpm state нет.

Package install-check выполняет `pi --version`. Интерактивная работа с gateway, моделями и реальной
TUI-сессией остаётся в f8-02/f8-04.
