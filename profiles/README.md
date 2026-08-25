# Lattice Profiles

Каталог `profiles/` хранит пресеты и роли, собранные из [модулей](../modules/README.md).

Профиль объединяет повторяемые настройки в одну роль и избавляет от копипасты в конфигах [нод](../nodes/README.md). В отличие от `modules/`, профиль может задавать инфраструктурные дефолты конкретной сети Lattice: URL репозиториев, ветки деплоя, набор стандартных сервисов и другие shared-настройки.

Каждый профиль живет в отдельной директории, является самостоятельным flake и предоставляет `nixosModule` output.

## Профили

- [`gitops/`](./gitops/flake.nix) - pull-based деплой через `comin` для сети Lattice.
- [`networking/`](./networking/flake.nix) - общий реестр портов Lattice.
- [`radicle/`](./radicle/flake.nix) - seed node Radicle с `radicle-node` и HTTP gateway через `radicle-httpd`.
- [`rns-server/`](./rns-server/flake.nix) - Reticulum node server с Lattice AutoInterface-дефолтами.
- [`rnsh/`](./rnsh/flake.nix) - listener remote shell через Reticulum с общим RNS configDir.
- [`tcp-gateway/`](./tcp-gateway/flake.nix) - Caddy gateway, автоматически настраивающий роутинг для всех активных TCP/HTTP профилей на ноде (например, Radicle HTTP-gateway).
- [`base/`](./base/flake.nix) - базовый профиль, который подключает стандартные настройки ноды.
