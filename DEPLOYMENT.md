# Разворачивание Lattice

Разворачивание нод Lattice выполняется через `nixos-rebuild` из главного flake. В проекте не используется отдельный deploy-инструмент поверх NixOS-конфигураций.

Главный `flake.nix` — единственная точка сборки и единственный lock-файл. Локальные ноды находятся
в [nodes/](./nodes/README.md) как обычные NixOS-модули.

На нодах, где подключен профиль `profiles/gitops` напрямую или через `profiles/base`, дальнейшие обновления выполняются автоматически: агент `comin` периодически опрашивает `https://github.com/mytecor/lattice.git` и локальный Radicle repository path, ветку `main`, и применяет `nixosConfigurations.<hostname>`.

Для локальной ноды:

```sh
sudo nixos-rebuild switch --flake .#<node-name>
```

Для удаленной ноды по SSH:

```sh
nixos-rebuild switch --flake .#<node-name> --target-host root@<host> --use-remote-sudo
```

Имя `<node-name>` должно соответствовать записи в `nixosConfigurations` [flake.nix](./flake.nix),
которая собирает модуль конкретной ноды с общими слоями.

## Первая реальная нода: `mytecor-homelab`

Первой разворачивается нода `mytecor-homelab` на Intel N100. До начала установки нужно:

1. Создать `nodes/mytecor-homelab/` по образцу `nodes/example/`.
2. Задать `networking.hostName = "mytecor-homelab"` и экспортировать
   `nixosConfigurations.mytecor-homelab` из корневого flake.
3. Подключить Wi-Fi этой ноды через `lattice.wireless.networks`, передавая SSID и пароль путями
   `config.age.secrets.<name>.path`. Открытые значения Wi-Fi в Git и Nix store не добавляются.
4. Перед сборкой проверить, что ключ расшифрования `agenix` доступен целевой системе и Wi-Fi
   secrets расшифровываются в runtime-файлы.

Wi-Fi — единственный сетевой канал `mytecor-homelab`: Ethernet и резервного подключения нет.
Поэтому установка не считается завершённой, пока целевая конфигурация сама не подключается к
Wi-Fi без ручного создания NetworkManager-профиля.

Перед отключением локальной консоли нужно проверить:

- NetworkManager подключён к ожидаемой Wi-Fi-сети;
- работают DNS и HTTPS-доступ к `github.com`;
- `comin` видит remote `origin` и может получить ветку `main`;
- после перезагрузки Wi-Fi поднимается автоматически, а `comin` продолжает опрашивать remote.

Ручное подключение к Wi-Fi в installer environment допустимо только для самой установки. Оно не
заменяет Wi-Fi-конфигурацию установленной системы.
