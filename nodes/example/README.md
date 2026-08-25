# Lattice Node Example

Шаблон ноды с подключением слоев в рекомендуемом порядке.

Используйте эту ноду как основу для новых `nodes/<name>`.

Нода подключает выбранное железо напрямую как автономный hardware flake, например `hardware/intel-n100`, и импортирует его через `hardware.nixosModule`.

Storage описан локально в `disko.nix`: EFI-раздел, Btrfs volume с меткой `root`, subvolume `@root` для `/`, `@nix` для `/nix` и `@persist` для `/persist`.

`ephemeral-root.nixosModule` пересоздает `@root` при загрузке и сохраняет нужные данные в `/persist` через impermanence.

`base-profile.nixosModule` подключает базовый профиль ноды, включая `comin` для pull-based обновлений из GitHub и локального Radicle repo.

`radicle-profile.nixosModule` включает seed node Radicle и HTTP gateway. В конфиге ноды задается публичный ключ `services.radicle.publicKey`.

Общие слои `profiles/` и `modules/` подключаются отдельными flake inputs из основного репозитория.
