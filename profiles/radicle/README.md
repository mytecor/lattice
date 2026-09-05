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

## Bootstrap репозитория

Реестр репозиториев находится в [`repositories.nix`](./repositories.nix). Для Lattice профиль:

- оставляет общий default policy равным `block`;
- закрепляет RID в HTTP gateway;
- запускает `radicle-seed-lattice` после `radicle-node`;
- выполняет `rad seed --scope followed`, повторяя неуспешную попытку через минуту.

Bootstrap получает репозиторий от любого уже подключённого seed, который его хранит. Поэтому до
развёртывания чистой ноды хотя бы один доступный seed должен получить актуальную Radicle-реплику.
`scope = followed` ограничивает репликацию делегатами репозитория и явно followed peers.

`profiles/gitops` читает каноническую ветку из bare repository
`/var/lib/radicle/storage/<RID>` первым remote `comin`. GitHub остаётся вторым независимым remote:
на чистой ноде он обеспечивает первоначальное применение конфигурации, пока bootstrap ещё не
создал локальное Radicle storage. После появления storage `comin` может продолжать обновляться
при недоступном GitHub.

Для ноды со стираемым root весь `/var/lib/radicle` должен сохраняться в `/persist`.
