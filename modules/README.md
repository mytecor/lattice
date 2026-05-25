# Lattice Modules

Каталог `modules/` хранит локальные flake-репозитории NixOS-модулей, которые подключаются конкретными нодами.

## Модули

- [`storage-persistence/`](./storage-persistence/README.md) - настройка постоянных данных через `nix-community/impermanence` и `/persist`.
- [`wipe-root/`](./wipe-root/README.md) - пересоздание Btrfs root subvolume при загрузке initrd.

## Правило

Каждый каталог в `modules/` должен быть самостоятельным flake и предоставлять `nixosModule` output.
