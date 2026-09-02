# Ephemeral Root

Модуль включает схему ephemeral root для Btrfs: root subvolume пересоздается при загрузке, а нужные системные данные сохраняются через `nix-community/impermanence`.

## Модульность и границы

Стираемый root — поддерживаемая, но опциональная возможность ноды. Модуль предоставляет общий
механизм и минимальный persistence, не выбирая диск и не задавая полную разметку. Конкретная нода
подключает модуль явно, описывает storage в `nodes/<name>/disko.nix` и дополняет persistence своими
ключами, идентичностями и данными сервисов в `nodes/<name>/config.nix`.

Hardware-модули и общие профили не должны включать стираемый root неявно: это позволило бы
конфигурации железа управлять жизненным циклом данных и сделало бы схему обязательной для всех нод.

## Что делает

- Подключает `nix-community/impermanence`.
- Монтирует top-level Btrfs volume в initrd.
- Атомарно переносит предыдущий root в отдельный каталог и создаёт новый subvolume перед
  монтированием `sysroot`.
- При ошибке пытается вернуть предыдущий root и, по умолчанию, продолжить загрузку в degraded mode.
- После успешной загрузки удаляет лишние старые roots вне критического пути initrd.
- Настраивает `environment.persistence."/persist"` для базовых системных данных.

## Опции

- `lattice.ephemeral-root.enable` - явно включает механизм; по умолчанию модуль инертен.
- `lattice.ephemeral-root.subvolume` - имя root subvolume, по умолчанию `@root`.
- `lattice.ephemeral-root.device` - устройство top-level Btrfs volume; по умолчанию берётся из
  `fileSystems."/".device`.
- `lattice.ephemeral-root.oldRootsDirectory` - каталог для выведенных из эксплуатации roots, по
  умолчанию `@old-roots`.
- `lattice.ephemeral-root.retainedRoots` - сколько предыдущих roots оставить после успешной
  загрузки, по умолчанию один.
- `lattice.ephemeral-root.failureMode` - `continue` пытается сохранить доступность при сбое
  ротации, `emergency` блокирует mount root до успешного завершения операции.

## Требования

Разметка root должна использовать Btrfs и монтировать subvolume, заданный в
`lattice.ephemeral-root.subvolume`. Требуется `btrfs-progs` версии 6.12 или новее, поскольку очистка
использует безопасное рекурсивное удаление штатной командой Btrfs.

Нода должна монтировать `/persist` во время загрузки. Для Btrfs-разметки через `disko` это обычно задается в `nodes/<name>/disko.nix` через subvolume `@persist` и `fileSystems."/persist".neededForBoot = true`.

Перед включением модуля нода обязана перечислить данные, которые действительно должны переживать
перезагрузку: ключ расшифрования secrets, необходимое для административного доступа состояние и
только те идентичности сервисов, которым требуется стабильность. Например, Reticulum identity
может быть объявлена одноразовой и не включаться в persistence. Эти решения являются частью
конфигурации ноды, а не универсального модуля.

## Подключение в корневом flake

```nix
inputs.module-ephemeral-root = {
  url = "path:./modules/ephemeral-root";
  flake = false;
};

outputs = { nixpkgs, module-ephemeral-root, ... }: {
  nixosConfigurations.example = nixpkgs.lib.nixosSystem {
    modules = [
      "${module-ephemeral-root}"
      {
        lattice.ephemeral-root = {
          enable = true;
          subvolume = "@root";
          device = "/dev/disk/by-label/root";
          retainedRoots = 1;
          failureMode = "continue";
        };
      }
    ];
  };
};
```

## Проверка

`nix flake check --all-systems --no-build` проверяет включённую и выключенную конфигурации модуля.
Полная сборка `checks.x86_64-linux.example` подтверждает, что rotate/prune scripts и initrd unit
попадают в system closure.

На Linux с root-доступом алгоритм можно проверить на временном loopback-Btrfs без изменения
реальных дисков:

```sh
sudo tests/ephemeral-root-loop.sh <rotate-script> <prune-script>
```

Тест проверяет новый root, сохранность `@nix` и `@persist`, вложенный subvolume, безопасный отказ и
рекурсивную очистку с retention. Оба пути скриптов доступны в `ExecStart` соответствующих units
после сборки конфигурации.
