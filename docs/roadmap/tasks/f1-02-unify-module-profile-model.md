# Привести модель flake/не-flake `modules` и `profiles` к одному решению

Фича: [F1 — одна железная нода](../FEATURES.md).

## Контекст

Документация (`modules/README.md`, `profiles/README.md`) требует, чтобы каждый каталог был
самостоятельным flake с `nixosModule` output. На диске лежат только `config.nix`/`options.nix`, а
`nodes/example` подключает их как `flake = false`. Нужно выбрать одну модель и привести к ней и
код, и доки. Решение №1 уже согласовано в [BUILD_MODEL.md](../../../BUILD_MODEL.md): модули
приходят как inputs с `flake = false`, типизированные опции вместо скрытых аргументов.

## Что сделать

- [ ] Зафиксировать итог решения №1 в `ARCHITECTURE.md` и удалить `BUILD_MODEL.md`.
- [ ] Привести `modules/README.md` и `profiles/README.md` в соответствие с фактической моделью
      (`flake = false`, inputs, а не импорты).
- [ ] Убрать скрытые аргументы (`rnsServerPackage`, `rnshPackage`, `latticePorts`) в пользу
      типизированных опций (`types.package`, `types.port`).
- [ ] Подтвердить, что реестр `profiles/networking/ports.nix` сохраняется как источник значений.

## Критерий готовности

- [ ] Документация и код описывают одну и ту же модель сборки.
- [ ] Модули и профили подключаются через inputs с `flake = false` без скрытых аргументов.

## Затрагиваемые файлы / слои

- `modules/README.md`, `profiles/README.md`
- `modules/rns-server`, `modules/rnsh` (аргументы → опции)
- `ARCHITECTURE.md`, `BUILD_MODEL.md`

## Открытые вопросы

Перенос решения №1 из `BUILD_MODEL.md` в `ARCHITECTURE.md` — внутри этой задачи.
