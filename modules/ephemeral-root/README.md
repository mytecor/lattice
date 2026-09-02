# Ephemeral Root

Модуль включает схему ephemeral root для Btrfs: root subvolume пересоздается при загрузке, а нужные системные данные сохраняются через `nix-community/impermanence`.

## Что делает

- Подключает `nix-community/impermanence`.
- Монтирует top-level Btrfs volume в initrd.
- Удаляет и заново создает root subvolume перед монтированием `sysroot`.
- Настраивает `environment.persistence."/persist"` для базовых системных данных.
- Добавляет persistence mount points для `/data` и `/var/cache`.

## Опции

- `lattice.ephemeral-root.subvolume` - имя root subvolume, который нужно пересоздавать, например `@root`.
- `lattice.ephemeral-root.device` - устройство top-level Btrfs volume.

## Требования

Разметка диска должна использовать Btrfs, а нода должна задать `lattice.ephemeral-root.device`.

Нода должна монтировать `/persist` во время загрузки. Для Btrfs-разметки через `disko` это обычно задается в `nodes/<name>/disko.nix` через subvolume `@persist` и `fileSystems."/persist".neededForBoot = true`.

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
        lattice.ephemeral-root.subvolume = "@root";
        lattice.ephemeral-root.device = "/dev/disk/by-label/root";
      }
    ];
  };
};
```
