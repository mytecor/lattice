# mytecor-homelab

Первая физическая нода Lattice на Intel N100. До миграции машина использует hostname `byurik`;
после установки имя каталога, `networking.hostName` и `nixosConfigurations` совпадают:
`mytecor-homelab`.

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
