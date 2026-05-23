# Nodes

Каталог `nodes/` хранит локальные flake-репозитории конкретных нод Lattice.
Нода должна быть самостоятельным flake, который полностью описывает свою NixOS-конфигурацию.

## Назначение

Каждая нода является отдельным flake и сама собирает свою итоговую NixOS-конфигурацию. Главный flake проекта только импортирует нодовые flake и прокидывает их `nixosConfigurations` для разворачивания через `nixos-rebuild`.

## Типичная структура

- `flake.nix` - flake конкретной ноды
- `flake.lock` - зафиксированные версии зависимостей ноды
- `configuration.nix` - основной состав ноды
- `secrets/` - зашифрованные секреты, ключи и материалы, привязанные к этой ноде
- `files/` - дополнительные файлы для разворачивания на узле
- `README.md` - заметки по этой конкретной ноде

## Что хранить

- собственный `flake.nix` ноды
- inputs, нужные именно этой ноде
- выбранное hardware
- подключенные profiles
- точечные modules
- локальные переопределения
- нодовые secrets и files

## Минимальный flake ноды

```nix
{
  description = "Lattice node";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = inputs@{ nixpkgs, disko, ... }: {
    nixosConfigurations.node-name = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      specialArgs = { inherit inputs; };

      modules = [
        disko.nixosModules.disko
        ./configuration.nix
      ];
    };
  };
}
```

## Порядок сборки

1. Flake выбирает `nixpkgs` и внешние зависимости.
2. Flake подключает hardware, profiles, modules и secrets.
3. Flake экспортирует `nixosConfigurations.<node-name>`.
