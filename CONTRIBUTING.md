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

При смене ключей следуйте [инструкции ротации и отзыва](./KEY_MANAGEMENT.md). Она задаёт порядок
обновления `secrets.nix`, перешифрования `.age`, передачи нового ключа и проверки доступа до
удаления старого. Публикуйте правила и шифротексты одной фазы одним commit; не добавляйте в Git
закрытые ключи, открытые значения или вывод их расшифрования. Смена получателей не меняет сами
секреты и не является достаточным отзывом после компрометации.

Перед отправкой изменений проверьте вычисление всех выходов flake:

```sh
nix flake check --all-systems --no-build
```

Полная проверка `example` собирает NixOS system closure и одновременно подтверждает, что
`profiles/base` включил `comin`, оптимизацию Nix store и автоматический garbage collection:

```sh
nix build .#checks.x86_64-linux.example
nix build .#checks.x86_64-linux.mytecor-homelab
```

Для полной проверки нужен `x86_64-linux` builder. В pull request и при push в `main` её выполняет
GitHub Actions.

Стираемый root дополнительно проверяется evaluation-check `checks.x86_64-linux.ephemeral-root-module`
и привилегированным loopback-тестом `tests/ephemeral-root-loop.sh`. Loopback-тест запускается только
на Linux и не обращается к реальным дискам.
