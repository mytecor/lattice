# Storage Persistence

Модуль для обеспечения постоянства данных между перезагрузками (impermanence).

## Что делает

- Подключает `nix-community/impermanence`.
- Настраивает `environment.persistence."/persist"`.
- Сохраняет базовые системные директории и файлы, которым нужно переживать очистку root subvolume.

## Требования

Нода должна монтировать `/persist` во время загрузки. Для Btrfs-разметки через `disko` это обычно задается в `nodes/<name>/disko.nix` через subvolume `@persist` и `fileSystems."/persist".neededForBoot = true`.

## Подключение

```nix
inputs.persistence.url = "path:../../modules/storage-persistence";

outputs = { nixpkgs, persistence, ... }: {
  nixosConfigurations.example = nixpkgs.lib.nixosSystem {
    modules = [
      persistence.nixosModule
    ];
  };
};
```
