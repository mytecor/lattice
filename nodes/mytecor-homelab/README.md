# mytecor-homelab

Первая физическая нода Lattice на Intel N100. До миграции машина использует hostname `byurik`;
после установки имя каталога, `networking.hostName` и `nixosConfigurations` совпадают:
`mytecor-homelab`.

Статус: развёрнута 2026-09-03, доступна как `mytecor-homelab.local` и автоматически применяет
GitHub `main` через `comin`.
Reticulum/rnsh проверены через публичные TCP peers Sydney и ReticulumNet.
После перезагрузки в Generation 7 (2026-09-04, `e934ee9`) подтверждены автоматическое
подключение Wi-Fi, SSH и rnsh с прежними identity и destination; состояние Reticulum
сохранилось в `/persist`. Подробности — в [f3-04](../../docs/roadmap/tasks/f3-04-rnsh-nat-access.md).
2026-09-05 `comin` применил `ba03779` без перезагрузки: работающие `rnsh` и `rns-server`
используют upstream snapshot `042e37047b70`, оба сервиса активны и не перезапускались.
После переключения Mac на мобильный hotspot rnsh-доступ с прежними identity и destination
повторно проверен через публичные peers без общей LAN; PID и restart counters сервисов не
изменились.

## Hardware

- CPU: Intel N100
- disk: `/dev/disk/by-id/ata-EAGET_SSD_512GB_EAGET20250114W00252`
- network: Wi-Fi через NetworkManager
- boot: UEFI + systemd-boot

## Bootstrap

Закрытый age-ключ не хранится в Git. До разрушительного шага файл
`.secrets/mytecor-homelab.agekey` передаётся по существующему SSH-каналу в
`/persist/var/lib/lattice/age/identity` с режимом `0600`.

Wi-Fi создаётся декларативно из `wifi-ssid.age` и `wifi-password.age`. Доступ SSH разрешён только
по ключу Mac, а host key создаётся непосредственно в `/persist/etc/ssh/`.

Reticulum identity исходной системы не переносится.

Плановая смена age-ключа, откат и действия при компрометации описаны в
[KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md). Ротация ключа расшифрования не требует менять
Wi-Fi-пароль, SSH host key или имя ноды; при утечке доступные через ключ секреты заменяются.

## Reticulum и rnsh

Корневой flake подключает `profiles/rns-network` и `profiles/rnsh`. Homelab использует публичные
peers Sydney/ReticulumNet как исходящие TCP uplink. Transport routing включён для передачи
анонсов и соединений локального rnsh. На firewall по-прежнему открыт только SSH-порт 22.

Слушатель работает как пользователь `rnsh` без sudo/root-привилегий. Его destination:
`4cf57c92d739f498d2d007b79da66624`. Этот адрес получен по доверенному SSH-каналу; fingerprint
не следует принимать заново из недоверенного сетевого анонса при смене identity.
Сервисная identity находится в `/var/lib/rnsh/identity`, каталог сохраняется в `/persist`.
Состояние транспорта `/var/lib/rns` также сохраняется. Отдельный ключ оператора хранится
на Mac в `.secrets/rnsh-operator/identity`; файл не входит в Git.
Разрешённый initiator hash: `59bfffc440ddc304749fd9477865b811`.

На Mac с Python RNS 1.5.2 создайте конфиг из общего реестра (из корня репозитория):

```sh
umask 077
mkdir -p .secrets/rns-client
nix eval --impure --raw --file scripts/rns-client-config.nix > .secrets/rns-client/config
rnsd --config .secrets/rns-client
```

Оставьте daemon работающим и из второго интерактивного терминала подключитесь:

```sh
rnsh --config .secrets/rnsh-operator \
  --rnsconfig .secrets/rns-client \
  --identity .secrets/rnsh-operator/identity \
  4cf57c92d739f498d2d007b79da66624
```

Клиент использует отдельный shared instance и порты 39428/39429, чтобы не менять пользовательский
`~/.reticulum`. Не генерируйте заново операторскую identity поверх существующей: новая identity
потребует обновить allowlist на ноде. Python rnsh следует запускать из терминала с TTY.

## Root password

Пароль root опционально задаётся через зашифрованный `root-password-hash.age`. В secret хранится
только yescrypt-хеш, а не открытый пароль. Пока файла нет, парольный вход root заблокирован.

Сгенерировать хеш интерактивно, не добавляя пароль в shell history:

```sh
umask 077
nix shell nixpkgs#mkpasswd -c mkpasswd -m yescrypt \
  > /tmp/mytecor-root-password.hash
```

Из каталога `nodes/mytecor-homelab/secrets/` зашифровать хеш для ноды и recovery SSH-ключа Mac:

```sh
nix shell nixpkgs#age -c age \
  -r age1dyxfyhf8s5lj9k0pzkkjjte0dcg4yecwglh88kmv2udau0q33v0ssa4pd8 \
  -R ~/.ssh/byurik.pub \
  -o root-password-hash.age \
  /tmp/mytecor-root-password.hash

rm /tmp/mytecor-root-password.hash
```

После добавления `root-password-hash.age` в Git конфигурация автоматически подключит его как
`users.users.root.hashedPasswordFile`. `users.mutableUsers = false` восстанавливает заданный hash
при каждой активации, в том числе после очистки root. SSH остаётся key-only: пароль предназначен
для локальной консоли и `su`, а `services.openssh.settings.PasswordAuthentication` остаётся `false`.

## Миграция

Миграция выполняется из работающего `byurik` без kexec, чтобы не разрывать Wi-Fi до завершения
установки. После публикации конфигурации в GitHub `main`:

1. Клонировать этот commit на ноду.
2. Передать `.secrets/mytecor-homelab.agekey` во временный файл под `/run/lattice-bootstrap/`.
3. Выполнить `scripts/check-mytecor-homelab-migration.sh`.
4. Выполнить `scripts/install-mytecor-homelab.sh` с указанной в нём confirmation phrase.
5. Проверить установленную систему и только затем отдельно выполнить `reboot`.

Install script не форматирует работающий Btrfs и не удаляет старый top-level root. Он создаёт
целевые subvolumes рядом с ним, поэтому Wi-Fi и SSH продолжают работать до перезагрузки.
