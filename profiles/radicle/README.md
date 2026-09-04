# Radicle

Профиль включает seed node и HTTP gateway. Сейчас он не подключён к существующим нодам;
развёртывание Radicle и bootstrap его хранилища запланированы в
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

При подключении профиля в F4 нужно также настроить сохранение `/var/lib/radicle` в `/persist`
для ноды со стираемым root и проверить запуск с реальной парой ключей. Закрытый ключ Radicle
не должен совпадать с age-ключом или SSH host key.
