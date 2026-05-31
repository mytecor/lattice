# Lattice Nodes

Каталог `nodes/` хранит локальные flake-репозитории конкретных нод.
Нода должна быть самостоятельным flake, который полностью описывает свою NixOS-конфигурацию.

## Назначение

Каждая нода является отдельным flake и сама собирает свою итоговую NixOS-конфигурацию. Главный flake проекта только импортирует нодовые flake и прокидывает их `nixosConfigurations` для разворачивания через `nixos-rebuild`.

## Типичная структура

- `flake.nix` - flake конкретной ноды
- `flake.lock` - зафиксированные версии зависимостей ноды
- `config.nix` - основной состав ноды
- `disko.nix` - разметка диска, файловые системы и boot loader, если нода управляет storage через `disko`
- `secrets/` - зашифрованные секреты, ключи и материалы, привязанные к этой ноде
- `files/` - дополнительные файлы для разворачивания на узле
- `README.md` - заметки по этой конкретной ноде

Шаблон ноды можно посмотреть в [example/](./example/README.md)

## Что хранить

- собственный `flake.nix` ноды
- inputs, нужные именно этой ноде
- выбранное hardware
- storage-разметку конкретного диска
- подключенные profiles
- точечные modules
- локальные переопределения
- нодовые secrets и files

Hardware подключается как отдельный flake input конкретной платформы и используется через `hardware.nixosModule`.
Storage-разметка задается на уровне ноды. Для Btrfs и ephemeral root нода подключает `disko.nixosModules.disko`, локальный `disko.nix` и модуль `modules/ephemeral-root`.
Общий pull-based деплой подключается через модуль `modules/gitops-deploy`, чтобы ноды опрашивали удаленный репозиторий и применяли свою `nixosConfigurations.<hostname>`.

## Порядок сборки

1. Flake выбирает `nixpkgs` и внешние зависимости.
2. Flake подключает hardware, storage, profiles, modules и secrets.
3. Конфиг ноды задает локальные параметры вроде `networking.hostName`.
4. Flake экспортирует `nixosConfigurations.<node-name>`.
