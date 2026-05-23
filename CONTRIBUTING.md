# Участие в Lattice

## Добавление новой ноды

Главный [flake.nix](./flake.nix) является агрегатором нод. Каждая нода может быть отдельным flake в [nodes/](./nodes/README.md) или внешнем Git-репозитории. Вся логика конкретной ноды, включая `nixpkgs`, `disko`, hardware, profiles, modules и secrets, должна находиться в flake самой ноды.

Источник может быть локальной папкой или Git-репозиторием:

```nix
{
  inputs = {
    node-name.url = "./nodes/node-name";
    # node-name.url = "git+ssh://git@example.org/lattice/node-name.git";
  };

  outputs = inputs: {
    nixosConfigurations.node-name = inputs.node-name.nixosConfigurations.node-name;
  };
}
```
