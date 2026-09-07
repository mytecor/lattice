# Radicle

Профиль включает selective seed node, HTTP gateway и идемпотентный bootstrap репозитория
Lattice. На `mytecor-homelab` он подключён в рамках
[f4-01](../../docs/roadmap/tasks/f4-01-radicle-seed-comin.md).

Нода, подключающая `profiles/radicle/config.nix`, обязана предоставить:

- `services.radicle.publicKey` — соответствующий публичный SSH-ключ Radicle без комментария.
  Его можно хранить в Git; профиль не задаёт фиктивный ключ по умолчанию.
- `age.secrets.radicle-private-key.file` — путь к зашифрованному `.age`-файлу с отдельным
  закрытым SSH-ключом Radicle, обычно созданным через `rad auth`.
  Получатели — age-ключ ноды и recovery-ключ администратора.

Объявление секрета в конфигурации ноды:

```nix
age.secrets.radicle-private-key = {
  file = ./secrets/radicle-private-key.age;
  mode = "0400";
};
```

Профиль передаёт `config.age.secrets.radicle-private-key.path` в `services.radicle.privateKey`.
Модуль NixOS загружает файл через systemd `LoadCredential` только в `radicle-node`; HTTP gateway
не получает закрытый ключ. Секрет остаётся доступен только root до передачи сервису;
его содержимое не читается при вычислении Nix и не попадает в Nix store.

Если ключ защищён парольной фразой, её также нужно доставить как systemd credential:
`services.radicle.privateKeyPassphrase` принимает имя credential, а не путь к файлу. Для
`agenix` можно объявить отдельный секрет `radicle-passphrase` и настроить:

```nix
systemd.services.radicle-node.serviceConfig.LoadCredential = [
  "dev.radicle.node.passphrase:${config.age.secrets.radicle-passphrase.path}"
];
```

Закрытый ключ Radicle не должен совпадать с age-ключом или SSH host key.

## HTTP API в LAN

При подключённом `profiles/tcp-gateway` Radicle HTTP API доступен через Caddy на порту 80:

```text
http://radicle.<node-name>.local/
```

Avahi публикует hostname через mDNS. Внутренний listener `radicle-httpd` остаётся на loopback и
напрямую в firewall не открывается.

## Bootstrap репозитория

Реестр репозиториев находится в [`repositories.nix`](./repositories.nix). Для Lattice профиль:

- оставляет общий default policy равным `block`;
- закрепляет RID в HTTP gateway;
- запускает `radicle-seed-lattice` после `radicle-node`;
- выполняет `rad seed --scope followed`, повторяя неуспешную попытку через минуту.

Bootstrap получает репозиторий от любого уже подключённого seed, который его хранит. Поэтому до
развёртывания чистой ноды хотя бы один доступный seed должен получить актуальную Radicle-реплику.
`scope = followed` ограничивает репликацию делегатами репозитория и явно followed peers.

Репозиторий Lattice имеет public visibility; переход выполнен identity revision
`d28b1987d705c6684cdd6c745deae86cb452fc5c`. На 2026-09-05 репликация подтверждена через Iris,
Rosa и Heptapod, а отдельный чистый клиент получил `2c70a7f` с Rosa. Public visibility относится
к репозиторию и не делает закрытый ключ ноды публичным: он по-прежнему поступает только через
systemd credential.

`profiles/gitops` читает каноническую ветку из bare repository
`/var/lib/radicle/storage/<RID>` и GitHub через `lattice-comin-source-sync`. Сервис передаёт
`comin` локальную fast-forward ветку и нормализует force-push без изменения дерева выбранного
commit. На чистой ноде GitHub обеспечивает первоначальное применение конфигурации, пока bootstrap
ещё не создал локальное Radicle storage. После появления storage обновление может продолжаться
при недоступном GitHub.

Для ноды со стираемым root весь `/var/lib/radicle` должен сохраняться в `/persist`.
