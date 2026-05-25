# Lattice Modules

Каталог `modules/` хранит локальные flake-репозитории NixOS-модулей, которые подключаются конкретными нодами.

## Модули

- [`ephemeral-root/`](./ephemeral-root/README.md) - пересоздание Btrfs root subvolume при загрузке initrd и постоянные данные через `nix-community/impermanence`.

## Правило

Каждый каталог в `modules/` должен быть самостоятельным flake и предоставлять `nixosModule` output.
