# Разворачивание Lattice

Разворачивание нод Lattice выполняется через `nixos-rebuild` из главного flake. В проекте не используется отдельный deploy-инструмент поверх NixOS-конфигураций.

Главный `flake.nix` — единственная точка сборки и единственный lock-файл. Локальные ноды находятся
в [nodes/](./nodes/README.md) как обычные NixOS-модули.

На нодах, где подключен профиль `profiles/gitops` напрямую или через `profiles/base`, дальнейшие обновления выполняются автоматически: агент `comin` периодически опрашивает `https://github.com/mytecor/lattice.git` и локальный Radicle repository path, ветку `main`, и применяет `nixosConfigurations.<hostname>`.

Для локальной ноды:

```sh
sudo nixos-rebuild switch --flake .#<node-name>
```

Для удаленной ноды по SSH:

```sh
nixos-rebuild switch --flake .#<node-name> --target-host root@<host> --use-remote-sudo
```

Имя `<node-name>` должно соответствовать записи в `nixosConfigurations` [flake.nix](./flake.nix),
которая собирает модуль конкретной ноды с общими слоями.
