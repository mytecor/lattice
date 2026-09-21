# mytecor-homelab

Первая физическая нода Lattice на Intel N100. До миграции машина использует hostname `byurik`;
после установки имя каталога, `networking.hostName` и `nixosConfigurations` совпадают:
`mytecor-homelab`.

Статус: развёрнута 2026-09-03, доступна как `mytecor-homelab.local` и автоматически применяет
`main` через `comin`. Основной source remote — локальная Radicle-реплика; GitHub остаётся
независимым fallback.
Reticulum/rnsh проверены через публичные TCP peers Sydney и ReticulumNet.
После перезагрузки в Generation 7 (2026-09-04, `e934ee9`) подтверждены автоматическое
подключение Wi-Fi, SSH и rnsh с прежними identity и destination; состояние Reticulum
сохранилось в `/persist`. Подробности — в [f3-04](../../roadmap/f3-reticulum-tcp/f3-04-rnsh-nat-access.md).
2026-09-05 `comin` применил `ba03779` без перезагрузки: работающие `rnsh` и `rns-server`
используют upstream snapshot `042e37047b70`, оба сервиса активны и не перезапускались.
После переключения Mac на мобильный hotspot rnsh-доступ с прежними identity и destination
повторно проверен через публичные peers без общей LAN; PID и restart counters сервисов не
изменились. 2026-09-05 профиль прикладных сервисов проверен с Mac: Caddy вернул node-status JSON
и проксировал Radicle HTTP API; отдельный backend-процесс не используется.

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
анонсов и соединений локального rnsh. Для административного доступа открыт SSH 22; прикладной
gateway дополнительно открывает HTTP/HTTPS 80 и 443.

## Прикладной HTTP gateway

Профиль `profiles/app-services` публикует node-status endpoint через Caddy:

```text
http://status.mytecor-homelab.local/
```

Avahi публикует этот service-specific hostname в mDNS, поэтому с Mac достаточно выполнить:

```sh
curl --fail http://status.mytecor-homelab.local/
```

Ожидаемый ответ — `{"node":"mytecor-homelab","service":"lattice-node-status"}`. Отдельный
backend или внутренний listener для статического endpoint не запускается. Radicle HTTP API
доступен по тому же ingress-контракту:

```text
http://radicle.mytecor-homelab.local/
```

LLM gateway доступен через отдельный Caddy reverse proxy и публикуемый Avahi mDNS-алиас:

```text
http://llm-gateway.mytecor-homelab.local/v1
```

Сам Gateway (Lattice-owned Go proxy поверх Bifrost) слушает на `127.0.0.1:9208`; в LAN открыт
только Caddy на порту 80.

Grafana frontend (F12 observability) доступен через тот же Caddy ingress и mDNS-алиас:

```text
http://grafana.mytecor-homelab.local/
```

Сам Grafana слушает на loopback `127.0.0.1:9215`; в LAN открыт только Caddy на порту 80.
Admin-логин защищён паролем из agenix-секрета (см. `modules/grafana/README.md`).

Authentik (центральный SSO, F14) доступен через тот же Caddy ingress:

```text
http://auth.mytecor-homelab.local/
```

Сам Authentik слушает loopback `127.0.0.1:9220`; в LAN открыт только Caddy на порту 80.
Входы пользовательских сервисов, подключённых к SSO (ForwardAuth или нативный OIDC),
ведут на эту логин-страницу как на единственную точку входа. Секреты (SECRET_KEY,
bootstrap token, bootstrap password) — только agenix runtime-файлами через EnvironmentFile,
см. [modules/authentik/README.md](../../modules/authentik/README.md).

### Оператор: живое подтверждение Authentik (F14)

Всё развёрнуто декларативно (module, sso-профиль, секреты); живых шагов на ноде не выполнялось.
Для подтверждения на живой ноде выполните по порядку:

1. **Подготовка ноды**: `nixos-rebuild switch` (или comin-цикл) — реально поднимутся
   postgresql, юниты `authentik-migrate`/`server`/`worker`, Caddy-сайт `auth` и mDNS-алиас
   `auth-mdns`. Проверка юнитов и логов:

   ```sh
   sudo systemctl status authentik-server authentik-worker authentik-migrate
   sudo journalctl -u authentik-server -n 50 --no-pager
   ```

2. **Достижимость**: с Mac

   ```sh
   curl --fail http://auth.mytecor-homelab.local/if/flow/initial/       # страница логина
   curl --fail http://acp-ui.mytecor-homelab.local/                     # 302/401 до входа
   ```

3. **Оператор-аккаунт**: bootstrap-учётные данные лежат в agenix-секретах
   (`authentik-bootstrap-*`); токен — постоянный (intent=api, expiring=false) для
   декларативного provisioning. Войти: `http://auth.mytecor-homelab.local/if/flow/initial/`

4. **Provisioning провайдеров**: выполнить blueprint/скрипт из `tasks/f14/` (появится при
   создании) или вручную через REST с bootstrap-токеном: OIDC-провайдер (Grafana),
   ForwardAuth endpoint для acp-ui. Идемпотентно, источник истины — репозиторий.

5. **Проверка Grafana через SSO**: `http://grafana.mytecor-homelab.local/` → редирект на
   `auth`, вход → admin-роль из группы `authentik Admins`.

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

## Yggdrasil и mesh-доступ (f4-05)

Наружу нода доступна через
[Yggdrasil](https://yggdrasil-network.github.io/) — self-organizing IPv6 mesh-оверлей
с криптографическими адресами в `200::/7`. Исходящие подключения к публичным peers дают
достижимость адреса ноды из интернета даже за NAT, без белого IP и проброса портов.

Стабильная идентичность ноды лежит в agenix-секрете `yggdrasil-keys.age` (в Git только
`.age`-шифротекст). Секрет — PKCS8 PEM-приватный ключ Yggdrasil; `services.yggdrasil.settings.PrivateKeyPath`
указывает на расшифрованный файл, который Yggdrasil читает через systemd credentials
(`LoadCredential`), поэтому приватный ключ не попадает в Nix store.

Из адреса ноды выпущены публичные поддомены `*.homelab.myt.su` (AAAA-записи `homelab.myt.su`
и `*.homelab.myt.su` в зоне Cloudflare `myt.su`, DNS-only — не proxied, иначе Cloudflare
edge не смог бы доставить трафик до ygg-адреса; созданы через API токеном
`caddy-cloudflare-token` 2026-09-18):

```text
https://acp.homelab.myt.su/          — ACP (Pi)
https://acp-ui.homelab.myt.su/       — web-клиент ACP (f13-01, с 2026-09-20)
https://git-cache-proxy.homelab.myt.su/
https://radicle.homelab.myt.su/
https://status.homelab.myt.su/
```

Grafana и LLM gateway в mesh НЕ выводятся (`meshExclude`): у них нет публичной TLS/API-key
защиты, поэтому они остаются только на LAN-контракте `*.local`. Порт 80 (HTTP) открыт в
firewall; 443 открыт и mesh-сайты обслуживаются по HTTPS через DNS-01 ACME Cloudflare
(токен `caddy-cloudflare-token.age` подключён, проверено 2026-09-18).

### Внешний DNS

Нужны AAAA-записи `*.homelab.myt.su` (и сам `homelab.myt.su`) → yggdrasil-адрес ноды
(публичное значение, выводится из приватного ключа):

```text
address:    200:e9f0:e122:7db:3bea:cf88:cdf3:fb91
subnet:     300:e9f0:e122:7db::/64
publickey:  8b078f6efc12620a983b9906023747b01bc5c6e2464fc171299fb5044374fdf3
```

Записи видят только клиенты, находящиеся в yggdrasil-сети (сама нода и остальные участники
mesh-оверлея). Это mesh-доступ, а не публичный интернет.

### Проверка с клиента в yggdrasil-сети (Mac)

На Mac достаточно клиента Yggdrasil в той же сети (например, демон `yggdrasil` уже работает
с `/etc/yggdrasil.conf`). Из терминала:

```sh
curl --fail https://status.homelab.myt.su/
```

Ожидаемый ответ — тот же JSON, что и `http://status.mytecor-homelab.local/`. Любой сервис с
mesh-поддомена должен возвращать тот же ответ, что и его LAN-контракт:

```sh
curl --fail https://acp-ui.homelab.myt.su/   # HTML SPA, тот же контент, что и acp-ui.mytecor-homelab.local
```

### Создание/ротация секрета `yggdrasil-keys.age`

Пустой конфиг генерирует ключ и адрес (значения не печатать в консоль):

```sh
umask 077
# из каталога nodes/mytecor-homelab/secrets/
nix run nixpkgs#yggdrasil -- -genconf > /tmp/yggdrasil-new.conf
```

Публичный адрес/ключ (можно печатать — это не секрет):

```sh
yggdrasil -useconffile /tmp/yggdrasil-new.conf -address
```

Приватный ключ в PEM-формате (нужен для `PrivateKeyPath`) выводится через `-exportkey` и
шифруется прямо в `.age`, не попадая в консоль:

```sh
umask 077
# yggdrasil -exportkey печатает PEM на stdout; пайпим сразу в age --encrypt
nix run nixpkgs#yggdrasil -- -useconffile /tmp/yggdrasil-new.conf -exportkey | \
  nix run nixpkgs#age --encrypt \
    -r age1dyxfyhf8s5lj9k0pzkkjjte0dcg4yecwglh88kmv2udau0q33v0ssa4pd8 \
    -R ~/.ssh/mytecor-homelab.pub \
    -o yggdrasil-keys.age
rm /tmp/yggdrasil-new.conf
```

Адрес ноды в `200::/7` остаётся стабильным между перезагрузками, потому что ключ приходит из
agenix, а не генерируется заново. При ротации ключа нужно обновить DNS-записи
`*.homelab.myt.su` на новый адрес и перезапустить сервис (`systemctl restart yggdrasil`),
т.к. в этой версии agenix нет restartUnits.

## Radicle

Нода запускает selective seed и HTTP gateway с отдельной сервисной identity. Закрытый ключ
поступает из `radicle-private-key.age`, а `/var/lib/radicle` сохраняется в `/persist`.
Сервис `radicle-seed-lattice` автоматически разрешает и получает только репозиторий Lattice;
неуспешный bootstrap повторяется, пока репозиторий не станет доступен у подключённого seed.

Runtime проверен 2026-09-05:

- DID сервиса — `did:key:z6Mkvw9xTo5bXFHvQvR6csSC49MqNiK8oemNj7fxkJp5thJJ`;
- `radicle-node`, `radicle-httpd` и `radicle-seed-lattice` активны;
- policy RID `rad:z3AqC22BKQ5Gnrkw49N7PGJa91G6L` — `allow/followed`;
- `2c70a7f` доступен через Iris, Rosa и Heptapod и клонирован чистым клиентом с Rosa;
- `comin` выбрал `8533477` из `radicle/main` и успешно вычислил тот же system closure.

После применения конфигурации проверьте:

```sh
systemctl is-active radicle-node radicle-httpd radicle-seed-lattice
rad-system self --did
rad-system seed
git -C /var/lib/radicle/storage/z3AqC22BKQ5Gnrkw49N7PGJa91G6L rev-parse main
systemctl status lattice-comin-source-sync.timer
git -C /var/lib/comin/source/repository log -1 --oneline main
comin status
```

Полный журнал результатов и ещё открытый drill при недоступном GitHub зафиксированы в
[`f4-01`](../../roadmap/f4-payload/f4-01-radicle-seed-comin.md).

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
  -R ~/.ssh/mytecor-homelab.pub \
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
