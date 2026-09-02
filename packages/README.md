# Lattice Packages

Каталог `packages/` хранит пакеты, которых нет в общем репозитории Nix, локальные изменения, патчи и сборки внешних зависимостей.

Каждый пакет живёт в отдельной директории как функция для `callPackage`. Корневой flake подключает
каталог с `flake = false`, добавляет пакеты в overlay `pkgs.lattice` и экспортирует их через
`packages.${system}`. Пример — [`rns-rs/package.nix`](./rns-rs/package.nix).
