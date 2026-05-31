# Разворачивание Lattice

Разворачивание нод Lattice выполняется через `nixos-rebuild` из главного flake. В проекте не используется отдельный deploy-инструмент поверх NixOS-конфигураций.

Главный `flake.nix` является агрегатором нод. Каждая нода может быть отдельным flake в [nodes/](./nodes/README.md) или внешнем Git-репозитории.

На нодах, где подключен общий модуль [modules/gitops-deploy](./modules/gitops-deploy/README.md), дальнейшие обновления выполняются автоматически: агент периодически опрашивает `https://github.com/mytecor/lattice.git`, ветку `main`, и применяет `nixosConfigurations.<hostname>`.

Для локальной ноды:

```sh
sudo nixos-rebuild switch --flake .#<node-name>
```

Для удаленной ноды по SSH:

```sh
nixos-rebuild switch --flake .#<node-name> --target-host root@<host> --use-remote-sudo
```

Имя `<node-name>` должно соответствовать записи в `nixosConfigurations` [flake.nix](./flake.nix), которая импортирует конфигурацию из flake конкретной ноды.
