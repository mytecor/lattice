# Собрать `profiles/base` и подключить в `nodes/example`

Фича: [F1 — одна железная нода](../features/f1-one-node.md).

## Контекст

`profiles/base` объединяет GitOps, общий unfree-предикат и базовые системные дефолты. Итоговая
модель сборки зафиксирована в [ARCHITECTURE.md](../../../ARCHITECTURE.md): профиль подключается
корневым flake из общего `profiles` input с `flake = false`.

## Что сделать

- [x] Создать `profiles/base/` по образцу остальных профилей.
- [x] Подключить в `base` профиль `gitops`, unfree-предикат и базовые системные дефолты.
- [x] Подключить `profiles/base` к `nixosConfigurations.example` в корневом flake.
- [x] Добавить воспроизводимую `x86_64-linux` проверку system closure в flake и CI.
- [ ] Проверить, что профили действительно попадают в сборку (`nixos-rebuild build`).

## Критерий готовности

- [ ] `profiles/base` существует и собирается.
- [ ] `nodes/example` собирается с подключённым профилем.

## Затрагиваемые файлы / слои

- `profiles/base/` (новый)
- Корневой `flake.nix`, `nodes/example/default.nix`
- `DEPLOYMENT.md`, `profiles/README.md` (упоминания `base`)

## Открытые вопросы

Pure evaluation (`nix flake check --all-systems --no-build`) проходит. Полную сборку
`checks.x86_64-linux.example` выполняет GitHub Actions; локально она требует `x86_64-linux`
builder или запуска на целевом N100, потому что локальный host — `aarch64-darwin`.
