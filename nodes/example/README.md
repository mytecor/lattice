# Lattice Node Example

Шаблон локальной ноды. [`default.nix`](./default.nix) объединяет локальные storage и config;
порядок общих слоёв задаёт корневой [`flake.nix`](../../flake.nix).

Используйте эту ноду как основу для новых `nodes/<name>`.

Корневой flake подключает `hardware/intel-n100`, `disko`, impermanence и общие Lattice modules как
inputs с `flake = false`.

Storage описан локально в `disko.nix`: EFI-раздел, Btrfs volume с меткой `root`, subvolume `@root` для `/`, `@nix` для `/nix` и `@persist` для `/persist`.

Модуль `ephemeral-root` пересоздаёт `@root` при загрузке и сохраняет нужные данные в `/persist`
через impermanence.

Input `profiles` подключает каталог общих профилей с `flake = false`. Профиль `profiles/base`
включает `comin` для pull-based обновлений из GitHub и локального Radicle repo, разрешает
необходимые unfree-пакеты Reticulum и задаёт базовое обслуживание Nix store.

Профиль `profiles/radicle` можно добавить в корневой состав ноды: он включает seed node Radicle и
HTTP gateway, а конфиг ноды задаёт публичный ключ `services.radicle.publicKey`.

Общие слои `profiles/` и `modules/` подключаются обычными inputs с `flake = false` из основного
репозитория.
