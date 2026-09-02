# Участие в Lattice

## Добавление новой ноды

Главный [flake.nix](./flake.nix) — единственная точка сборки. Локальная нода добавляется как обычный
NixOS-модуль в `nodes/<name>/`: `default.nix` импортирует локальные `config.nix`, `disko.nix`,
secrets и files. Общие `nixpkgs`, hardware, profiles и modules выбираются в корневом flake.

Минимальное подключение новой локальной ноды:

```nix
{
  nixosConfigurations.node-name = nixpkgs.lib.nixosSystem {
    modules = [
      # Общие hardware/modules/profiles — по образцу example.
      ./nodes/node-name
    ];
  };
}
```

Внешние репозитории нод появятся в F5; они будут зависеть от экспортируемых Lattice modules и
overlay в одну сторону и не будут импортироваться обратно как вложенные flakes текущего checkout.

## Проверки

Перед отправкой изменений проверьте вычисление всех выходов flake:

```sh
nix flake check --all-systems --no-build
```

Полная проверка `example` собирает NixOS system closure и одновременно подтверждает, что
`profiles/base` включил `comin`, оптимизацию Nix store и автоматический garbage collection:

```sh
nix build .#checks.x86_64-linux.example
```

Для полной проверки нужен `x86_64-linux` builder. В pull request и при push в `main` её выполняет
GitHub Actions.
