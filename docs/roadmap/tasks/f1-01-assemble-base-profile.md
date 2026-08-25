# Собрать `profiles/base` и подключить в `nodes/example`

Фича: [F1 — одна железная нода](../FEATURES.md).

## Контекст

`profiles/base` упомянут в `profiles/README.md` и `DEPLOYMENT.md`, но не существует. В
`nodes/example/flake.nix` секция profiles закомментирована, поэтому ни один профиль в сборку
реально не попадает. По решению №1 в [BUILD_MODEL.md](../../../BUILD_MODEL.md) `profiles/base` =
gitops + unfree-предикат + базовые системные дефолты.

## Что сделать

- [ ] Создать `profiles/base/` по образцу остальных профилей.
- [ ] Подключить в `base` профиль `gitops`, unfree-предикат и базовые системные дефолты.
- [ ] Раскомментировать и подключить `profiles/base` в `nodes/example/flake.nix` (или в первый
      реальный узел).
- [ ] Проверить, что профили действительно попадают в сборку (`nixos-rebuild build`).

## Критерий готовности

- [ ] `profiles/base` существует и собирается.
- [ ] `nodes/example` собирается с подключённым профилем.

## Затрагиваемые файлы / слои

- `profiles/base/` (новый)
- `nodes/example/flake.nix`
- `DEPLOYMENT.md`, `profiles/README.md` (упоминания `base`)

## Открытые вопросы

_нет_
