# Lattice Modules

Каталог `modules/` хранит низкоуровневые NixOS-модули. Эти модули описывают системные механизмы и не должны хранить инфраструктурные дефолты сети Lattice, URL репозиториев, адреса конкретных сервисов или настройки конкретных нод.

## Модули

- [`ephemeral-root/`](./ephemeral-root/README.md) - безопасная ротация Btrfs root subvolume при загрузке и постоянные данные через `nix-community/impermanence`.
- [`llm-gateway/`](./llm-gateway/README.md) - Lattice Go proxy поверх Bifrost и безопасные
  credentials через systemd.
- [`pi/`](./pi/README.md) - версия и системное подключение воспроизводимой pnpm-сборки Pi + декларативная конфигурация `~/.pi/agent`.
- [`rns-server/`](./rns-server/README.md) - сервис `rns-server` и typed-генерация RNS ConfigObj-конфигов.
- [`rnsh/`](./rnsh/README.md) - listener-сервис для remote shell через Reticulum.
- [`wireless/`](./wireless/default.nix) - настройка Wi-Fi сетей через NetworkManager и runtime secret-файлы.
- [`wireless-hotspot/`](./wireless-hotspot/README.md) - concurrent Wi-Fi STA + AP на одном радио (hostapd + dnsmasq + NAT); см. требования rtw88 (`#channels <= 1`).
- [`git-cache-proxy/`](./git-cache-proxy/README.md) - read-only caching proxy для Git-репозиториев (f9-01): lazily клонирует bare mirror с origin и раздаёт дельту клиентам, cache на локальной POSIX FS не является source of truth.

## Правило

Каждый каталог содержит `default.nix` — обычный NixOS-модуль. Корневой flake подключает каталоги
как inputs с `flake = false` и экспортирует их через `nixosModules`. Зависимости на пакеты и значения
передаются типизированными опциями, а не скрытыми module arguments.
