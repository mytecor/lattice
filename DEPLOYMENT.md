# Разворачивание Lattice

Разворачивание нод Lattice выполняется через `nixos-rebuild` из главного flake. В проекте не используется отдельный deploy-инструмент поверх NixOS-конфигураций.

Главный `flake.nix` — единственная точка сборки и единственный lock-файл. Локальные ноды находятся
в [nodes/](./nodes/README.md) как обычные NixOS-модули.

На нодах, где подключен профиль `profiles/gitops` напрямую или через `profiles/base`, дальнейшие
обновления выполняются автоматически. `lattice-comin-source-sync` опрашивает каноническую `main`
в локальном Radicle storage и независимое зеркало
`https://github.com/mytecor/lattice.git`, затем публикует нормализованную локальную `main` для
штатного `comin`. Если upstream был force-pushed, нормализатор создаёт merge-коммит с прежним
deployment history и деревом нового upstream head. Поэтому вход `comin` всегда fast-forward, а
содержимое точно соответствует выбранному source commit.

Когда heads лежат в одной истории, выбирается более новый. При расхождении Radicle является
авторитетным, GitHub используется как fallback. Это предотвращает переключение ноды между двумя
несогласованными историями.

Чистая нода сначала получает конфигурацию через установочный checkout или GitHub. После запуска
`radicle-node` сервис `radicle-seed-lattice` получает репозиторий от доступного Radicle seed;
до успешного bootstrap отсутствующий локальный remote не мешает нормализатору использовать GitHub.
Подробности и runtime-проверки описаны в
[`profiles/radicle/README.md`](./profiles/radicle/README.md).

## Публикация в Radicle и GitHub

Git remotes хранятся в локальном `.git/config` и не переносятся в новый clone. Для рабочего
checkout один раз создайте общий remote `publish` с GitHub как fetch URL и двумя push URL:

```sh
git remote add publish https://github.com/mytecor/lattice.git
git remote set-url --add --push publish \
  rad://z3AqC22BKQ5Gnrkw49N7PGJa91G6L/z6Mkvq7AcVgfLmaecxQEasuErFk6s7fLDj2668WLBFCE9xWV
git remote set-url --add --push publish \
  https://github.com/mytecor/lattice.git
```

Обычная публикация выполняется одной командой:

```sh
git push publish main
```

Git последовательно отправляет commit в оба URL, но общей атомарной транзакции между Radicle и
GitHub нет. После частичного отказа сравните `refs/heads/main` в обоих remote и повторите push.
Локальный `origin/main` при push через `publish` может не обновиться; синхронизируйте tracking ref:

```sh
git fetch origin main
```

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

Для уже установленной ноды смена ключа и отзыв доступа описаны в
[KEY_MANAGEMENT.md](./KEY_MANAGEMENT.md). Не запускайте скрипт первоначальной установки ради
плановой ротации age-ключа.

## Первая реальная нода: `mytecor-homelab`

Первой разворачивается нода `mytecor-homelab` на Intel N100. До миграции эта физическая машина
работает с hostname `byurik`; миграция переименовывает её в `mytecor-homelab`, а не добавляет
`byurik` как отдельную Lattice-ноду. До начала установки нужно:

1. Создать `nodes/mytecor-homelab/` по образцу `nodes/example/`.
2. Задать `networking.hostName = "mytecor-homelab"` и экспортировать
   `nixosConfigurations.mytecor-homelab` из корневого flake.
3. Подключить Wi-Fi этой ноды через `lattice.wireless.networks`, передавая SSID и пароль путями
   `config.age.secrets.<name>.path`. Открытые значения Wi-Fi в Git и Nix store не добавляются.
4. Перед сборкой проверить, что ключ расшифрования `agenix` доступен целевой системе и Wi-Fi
   secrets расшифровываются в runtime-файлы.
5. Подключить модуль `ephemeral-root`, описать Btrfs subvolumes в node-local `disko.nix` и явно
   перечислить в persistence только ключи и данные, которые должны сохраниться.
6. До разрушительного шага подготовить в целевой системе Wi-Fi и SSH-доступ. После начала
   переключения процедура не должна зависеть от ввода данных или настройки доступа на самой ноде.

Wi-Fi — единственный сетевой канал `mytecor-homelab`: Ethernet и резервного подключения нет.
Поэтому установка не считается завершённой, пока целевая конфигурация сама не подключается к
Wi-Fi без ручного создания NetworkManager-профиля.

До разрушительного шага и первой перезагрузки нужно удалённо проверить:

- целевая конфигурация содержит автоматически создаваемый профиль ожидаемой Wi-Fi-сети;
- ключ расшифрования уже передан по действующему SSH-каналу в целевой persistent storage;
- SSH authorized keys и системный SSH-сервис входят в целевую конфигурацию;
- после смены hostname нода доступна по заранее известному DHCP reservation или по имени
  `mytecor-homelab.local`, не требуя поиска адреса на локальной консоли;
- работают DNS и HTTPS-доступ к `github.com`;
- `lattice-comin-source-sync` получает `main`, а `comin` видит локальный remote `source`;
- после перезагрузки Wi-Fi поднимается автоматически, а `comin` продолжает опрашивать remote.

Штатная миграция `byurik` в `mytecor-homelab` выполняется через уже работающий SSH-доступ. Она не
должна требовать ручного подключения к Wi-Fi, копирования ключей или настройки SSH на локальной
консоли — ни в installer environment, ни после первой загрузки. Локальная консоль является только
аварийным каналом восстановления и не входит в основной сценарий.

На исходной системе нет данных, требующих сохранения. Для первой миграции принято node-specific
решение не делать backup текущего root и не переносить существующую Reticulum identity. Это не
является общей политикой Lattice: для других нод необходимость backup определяется их данными и
планом отката. Откат `mytecor-homelab` в этой миграции — повторная чистая установка.

### Удалённая in-place установка

Для этой Wi-Fi-only ноды не используется стандартный kexec-путь `nixos-anywhere`: после смены
ядра installer environment не сможет восстановить Wi-Fi без отдельной локальной настройки.
Вместо этого новая система устанавливается в Btrfs subvolumes из работающего `byurik`; текущие
Wi-Fi и SSH остаются активны до финальной перезагрузки.

После публикации проверенного commit в GitHub `main` нужно клонировать тот же commit на ноду и
передать закрытый age-ключ во временный путь в `/run`. Затем выполнить read-only preflight:

```sh
sudo ./scripts/check-mytecor-homelab-migration.sh \
  /path/to/lattice \
  /run/lattice-bootstrap/mytecor-homelab.agekey
```

Проверка требует чистый checkout, совпадение `HEAD` с GitHub `main`, доступный Wi-Fi, правильные
устройства, UEFI, SSH host key, расшифровываемые secrets и собранный system closure.

Установка запускается только с точной confirmation phrase:

```sh
sudo ./scripts/install-mytecor-homelab.sh \
  INSTALL-MYTECOR-HOMELAB-ON-EAGET20250114W00252 \
  /path/to/lattice \
  /run/lattice-bootstrap/mytecor-homelab.agekey
```

Скрипт присваивает существующим файловым системам метки `ESP` и `root`, создаёт `@root`, `@nix` и
`@persist`, устанавливает заранее собранную систему, переносит age-ключ и текущий SSH host key в
`/persist`. Существующий top-level root не удаляется, а автоматическая перезагрузка не выполняется.
Перед ручным `reboot` остаётся возможность проверить новую boot entry и содержимое subvolumes по
действующему SSH-соединению.

После первой загрузки нужно отдельно проверить стираемый root: временный неперсистентный файл
исчезает после перезагрузки, а ключ secrets и необходимый административный доступ продолжают
работать. Стабильность Reticulum identity для этой ноды не проверяется. Механизм предоставляет
`modules/ephemeral-root`; разметка и список сохраняемых данных остаются ответственностью
конфигурации конкретной ноды.
