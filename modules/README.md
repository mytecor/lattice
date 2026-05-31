# Lattice Modules

Каталог `modules/` хранит локальные flake-репозитории NixOS-модулей, которые подключаются конкретными нодами.

## Модули

- [`ephemeral-root/`](./ephemeral-root/README.md) - пересоздание Btrfs root subvolume при загрузке initrd и постоянные данные через `nix-community/impermanence`.
- [`gitops-deploy/`](./gitops-deploy/README.md) - общий pull-based GitOps-деплой нод.
- [`rnsh/`](./rnsh/README.md) - listener-сервис для remote shell через Reticulum.
- [`rns-server/`](./rns-server/README.md) - сервис `rns-server` и typed-генерация RNS ConfigObj-конфигов.

## Правило

Каждый каталог в `modules/` должен быть самостоятельным flake и предоставлять `nixosModule` output.
