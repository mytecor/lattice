# Привести модель flake/не-flake `modules` и `profiles` к одному решению

Фича: [F1 — одна железная нода](./README.md).

## Контекст

Раньше документация требовала самостоятельный flake в каждом каталоге, а код содержал обычные
NixOS-модули и хрупкие parent-relative inputs вложенного flake ноды. Решение №1 зафиксировано в
[ARCHITECTURE.md](../../../ARCHITECTURE.md): один корневой flake, локальные слои приходят как inputs
с `flake = false`, зависимости задаются типизированными опциями вместо скрытых аргументов.

## Что сделать

- [x] Зафиксировать итог решения №1 в `ARCHITECTURE.md` и удалить `BUILD_MODEL.md`.
- [x] Привести `modules/README.md` и `profiles/README.md` в соответствие с фактической моделью
      (`flake = false`, inputs, а не импорты).
- [x] Убрать скрытые аргументы (`rnsServerPackage`, `rnshPackage`, `latticePorts`) в пользу
      типизированных опций (`types.package`, `types.port`).
- [x] Подтвердить, что реестр `profiles/networking/ports.nix` сохраняется как источник значений.

## Критерий готовности

- [x] Документация и код описывают одну и ту же модель сборки.
- [x] Модули и профили подключаются через inputs с `flake = false` без скрытых аргументов.

## Затрагиваемые файлы / слои

- `modules/README.md`, `profiles/README.md`
- `modules/rns-server`, `modules/rnsh` (аргументы → опции)
- `ARCHITECTURE.md`, корневой `flake.nix`

## Открытые вопросы

_нет_
