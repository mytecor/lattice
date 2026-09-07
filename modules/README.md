# Lattice Modules

Каталог `modules/` хранит низкоуровневые NixOS-модули. Эти модули описывают системные механизмы и не должны хранить инфраструктурные дефолты сети Lattice, URL репозиториев, адреса конкретных сервисов или настройки конкретных нод.

## Модули

- [`ephemeral-root/`](./ephemeral-root/README.md) - безопасная ротация Btrfs root subvolume при загрузке и постоянные данные через `nix-community/impermanence`.
- [`llm-gateway/`](./llm-gateway/README.md) - Lattice Go proxy поверх Bifrost, временный
  `token_proxy` compatibility runtime и credentials через systemd.
- [`rns-server/`](./rns-server/README.md) - сервис `rns-server` и typed-генерация RNS ConfigObj-конфигов.
- [`rnsh/`](./rnsh/README.md) - listener-сервис для remote shell через Reticulum.
- [`wireless/`](./wireless/default.nix) - настройка Wi-Fi сетей через NetworkManager и runtime secret-файлы.

## Правило

Каждый каталог содержит `default.nix` — обычный NixOS-модуль. Корневой flake подключает каталоги
как inputs с `flake = false` и экспортирует их через `nixosModules`. Зависимости на пакеты и значения
передаются типизированными опциями, а не скрытыми module arguments.
