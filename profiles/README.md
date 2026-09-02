# Lattice Profiles

Каталог `profiles/` хранит пресеты и роли, собранные из [модулей](../modules/README.md).

Профиль объединяет повторяемые настройки в одну роль и избавляет от копипасты в конфигах [нод](../nodes/README.md). В отличие от `modules/`, профиль может задавать инфраструктурные дефолты конкретной сети Lattice: URL репозиториев, ветки деплоя, набор стандартных сервисов и другие shared-настройки.

Каждый профиль живёт в отдельной директории как обычный NixOS-модуль. Корневой flake подключает
весь каталог `profiles/` одним input с `flake = false`; профиль может импортировать соседний профиль
и общий реестр значений внутри этого input.

## Профили

- [`gitops/`](./gitops/config.nix) - pull-based деплой через `comin` для сети Lattice.
- [`networking/`](./networking/ports.nix) - общий реестр портов Lattice.
- [`radicle/`](./radicle/config.nix) - seed node Radicle с `radicle-node` и HTTP gateway через `radicle-httpd`.
- [`rns-server/`](./rns-server/config.nix) - Reticulum node server с Lattice AutoInterface-дефолтами.
- [`rnsh/`](./rnsh/config.nix) - listener remote shell через Reticulum с общим RNS configDir.
- [`tcp-gateway/`](./tcp-gateway/config.nix) - Caddy gateway, автоматически настраивающий роутинг для всех активных TCP/HTTP профилей на ноде (например, Radicle HTTP-gateway).
- [`base/`](./base/default.nix) - базовый профиль: GitOps через `comin`, необходимые
  unfree-пакеты и стандартное обслуживание Nix store.
