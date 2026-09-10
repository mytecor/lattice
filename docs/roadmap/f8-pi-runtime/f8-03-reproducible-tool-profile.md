# Собрать воспроизводимый профиль tools для Pi

Фича: [F8 — интерактивный Pi runtime](./README.md). Зависит от f8-01.

## Контекст

Интерактивная нода и будущий worker должны получать один базовый контракт `bash/git/tools`, а
проектные зависимости — восстанавливать из репозитория.

## Что сделать

- [x] Определить минимальный базовый набор CLI tools и способ расширения из project flake/devShell.
- [x] Убрать зависимости от случайно установленных user/global packages.
- [x] Зафиксировать PATH, locale, Git identity boundary и рабочие каталоги.
- [x] Добавить smoke check команд из чистого окружения.

## Реализация (2026-09-10)

- Единый источник базового контракта — [`profiles/pi/base-tools.nix`](../../../profiles/pi/base-tools.nix):
  минимальный набор `bash/git/shell-инструменты/сетевые клиенты/редактор-поиск`. Импортируется
  и из `modules/pi/config.nix` (нода), и из `flake.nix` (devShell + пакет `pi-tool-profile`),
  поэтому нода и окружение разработчика видят одинаковый набор.
- Расширение без изменения рантайма: на ноде `lattice.pi.tools = [ "nodejs" … ]` (строки — имена
  атрибутов `pkgs`, либо package-значения); в разработке —
  `pkgs.mkShell { inputsFrom = [ pkgs.lattice.pi-develop-shell ]; packages = [ pkgs.nodejs ]; }`.
- На ноде tools попадают в `systemPackages` ровно из декларации tool profile
  (`base + lattice.pi.tools`), без зависимости от случайных user/global пакетов.
- Контракт окружения фиксируется в `/etc/pi.env` (inspectable, read-only): `PATH` из tool profile,
  `LANG`/`LC_ALL=C.UTF-8`, git identity boundary (`GIT_CONFIG_NOSYSTEM=1`,
  `GIT_CONFIG_GLOBAL=…/.gitconfig`).
- `flake.nix` экспортирует `devShells.<system>.default` и пакет `lattice.pi-tool-profile`.
- Smoke check из чистого окружения — [`tests/pi-tool-profile.nix`](../../../tests/pi-tool-profile.nix):
  проверяет состав профиля, реальный запуск ключевых команд из `env -i`, `git init/commit` с
  per-user identity и фиксацию locale. Проходит на x86_64-linux (проверено на homelab).
- NixOS-проверка [`tests/pi-config.nix`](../../../tests/pi-config.nix) расширена: tool profile в
  `systemPackages`, проектное расширение (`node`), `/etc/pi.env` с git-identity границей и locale.

## Критерий готовности

- [x] Базовый tool profile воспроизводится через Nix и проходит smoke check.
- [x] Новый проект может добавить tools декларативно, не меняя Pi runtime.

## Затрагиваемые файлы / слои

- `profiles/pi/`
- `flake.nix`
- документация разработки

## Открытые вопросы

_нет_.
