# Lattice Modules

Каталог `modules/` хранит локальные flake-репозитории низкоуровневых NixOS-модулей. Эти модули описывают системные механизмы и не должны хранить инфраструктурные дефолты сети Lattice, URL репозиториев, адреса конкретных сервисов или настройки конкретных нод.

## Модули

- [`ephemeral-root/`](./ephemeral-root/README.md) - пересоздание Btrfs root subvolume при загрузке initrd и постоянные данные через `nix-community/impermanence`.
- [`rns-server/`](./rns-server/README.md) - сервис `rns-server` и typed-генерация RNS ConfigObj-конфигов.
- [`rnsh/`](./rnsh/README.md) - listener-сервис для remote shell через Reticulum.
- [`wireless/`](./wireless/flake.nix) - настройка Wi-Fi сетей через NetworkManager и runtime secret-файлы.

## Правило

Каждый каталог в `modules/` должен быть самостоятельным flake и предоставлять `nixosModule` output.
