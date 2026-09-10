# Lattice Profiles

Каталог `profiles/` хранит пресеты и роли, собранные из [модулей](../modules/README.md).

Профиль объединяет повторяемые настройки в одну роль и избавляет от копипасты в конфигах [нод](../nodes/README.md). В отличие от `modules/`, профиль может задавать инфраструктурные дефолты конкретной сети Lattice: URL репозиториев, ветки деплоя, набор стандартных сервисов и другие shared-настройки.

Каждый профиль живёт в отдельной директории как обычный NixOS-модуль. Корневой flake подключает
весь каталог `profiles/` одним input с `flake = false`; профиль может импортировать соседний профиль
и общий реестр значений внутри этого input.

## Профили

- [`app-services/`](./app-services/README.md) - первый прикладной payload: node-status endpoint,
  опубликованный через `tcp-gateway` без отдельного backend-процесса.
- [`gitops/`](./gitops/config.nix) - pull-based деплой через `comin` для сети Lattice.
- [`llm-gateway/`](./llm-gateway/README.md) - безопасные production defaults для локального
  OpenAI-compatible gateway; upstreams и secret paths задаёт нода.
- [`networking/`](./networking/ports.nix) - общие порты и [публичные uplink Reticulum](./networking/reticulum.nix).
- [`radicle/`](./radicle/README.md) - seed node Radicle с `radicle-node`, HTTP gateway через `radicle-httpd` и закрытым ключом из `agenix`.
- [`rns-server/`](./rns-server/README.md) - Reticulum node server с AutoInterface, TCP listener и шаблоном TCP uplink.
- [`rns-network/`](./rns-network/README.md) - исходящие TCP-подключения к публичным узлам из общего реестра.
- [`rnsh/`](./rnsh/config.nix) - listener remote shell через Reticulum с общим RNS configDir.
- [`pi/`](./pi/README.md) - воспроизводимый tool profile для Pi-рантайма (f8-03): единый базовый
  контракт `bash/git/tools`, расширение проекта без изменения рантайма, фиксированные
  PATH/locale/git-identity и smoke check из чистого окружения.
- [`tcp-gateway/`](./tcp-gateway/README.md) - единый Caddy ingress на порту 80 для LAN-адресов
  `service.node-name.local`, публикуемых через mDNS/Avahi.
- [`base/`](./base/default.nix) - базовый профиль: GitOps через `comin`, необходимые
  unfree-пакеты и стандартное обслуживание Nix store.
