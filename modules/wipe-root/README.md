# Wipe Root

Модуль пересоздает выбранный Btrfs subvolume root во время initrd-загрузки.

## Что делает

- Монтирует top-level Btrfs volume.
- Удаляет subvolume, заданный в `lattice.wipe-root.subvolume`, если он существует.
- Создает subvolume заново перед монтированием `sysroot`.

## Опции

- `lattice.wipe-root.subvolume` - имя subvolume, который нужно пересоздавать, например `@root`.
- `lattice.wipe-root.device` - устройство top-level Btrfs volume. По умолчанию `/dev/disk/by-label/root`.

## Требования

Разметка диска должна использовать Btrfs и иметь метку `root`, либо нода должна явно задать `lattice.wipe-root.device`.

Сохраняемые данные должны лежать вне очищаемого subvolume, например в `/persist`, и монтироваться на ранней стадии загрузки.

## Подключение

```nix
inputs.wipe-root.url = "path:../../modules/wipe-root";

outputs = { nixpkgs, wipe-root, ... }: {
  nixosConfigurations.example = nixpkgs.lib.nixosSystem {
    modules = [
      wipe-root.nixosModule
      {
        lattice.wipe-root.subvolume = "@root";
      }
    ];
  };
};
```
