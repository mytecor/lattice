# Разворачивание Lattice

Разворачивание нод Lattice выполняется через `nixos-rebuild` из главного flake. В проекте не используется отдельный deploy-инструмент поверх NixOS-конфигураций.

Главный `flake.nix` является агрегатором нод. Каждая нода может быть отдельным flake в [nodes/](./nodes/README.md) или внешнем Git-репозитории.

Для локальной ноды:

```sh
sudo nixos-rebuild switch --flake .#<node-name>
```

Для удаленной ноды по SSH:

```sh
nixos-rebuild switch --flake .#<node-name> --target-host root@<host> --use-remote-sudo
```

Имя `<node-name>` должно соответствовать записи в `nixosConfigurations` [flake.nix](./flake.nix), которая импортирует конфигурацию из flake конкретной ноды.
