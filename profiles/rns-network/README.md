# Межузловая сеть Reticulum

Профиль `rns-network/config.nix` подключает узлы исходящими TCP-соединениями к публичному
Reticulum backbone. Собственный VPS и публичный IP для клиента не нужны. Профиль используется
вместо `rns-server/config.nix`: AutoInterface и входящий TCP listener отсутствуют, поэтому
межузловая проверка не может незаметно пройти через LAN. Transport routing и HTTP control plane
по умолчанию выключены.

## Общий реестр uplink

[networking/reticulum.nix](../networking/reticulum.nix) хранит выбранные адреса:

| Имя | Endpoint | Источник |
| --- | --- | --- |
| Sydney | `sydney.reticulum.au:4242` | [Объявление оператора](https://rns.recipes/forum/regional/sydney-australia) |
| ReticulumNet | `node.reticulumnet.nl:4242` | [Инструкция оператора](https://www.reticulumnet.nl/en/get-started/) |

Оба адреса проверены на TCP-доступность 2026-09-04. Публичные Backbone-интерфейсы совместимы
с TCPClientInterface; подключение нескольких peers соответствует
[рекомендации Reticulum](https://reticulum.network/manual/gettingstartedfast.html#connect-to-the-distributed-backbone).
Это добровольно поддерживаемая инфраструктура: наличие двух соединений уменьшает зависимость
от одного gateway, но не гарантирует независимость маршрутов и доступность.

Все ноды получают реестр через общий flake и `comin`; при bootstrap он входит в сборку.
Изменение peer выполняется в одном месте. Чтобы заменить реестр для отдельной ноды или теста:

```nix
lattice.rns-network.uplinks = {
  Primary = { host = "entry.example.net"; port = 4242; };
  Spare = { host = "spare.example.net"; enable = false; };
};
```

Это полная замена дефолтного реестра. Должен оставаться хотя бы один включённый peer. Названия
состоят из букв, цифр, дефиса и подчёркивания; порты — 1–65535. Для IPv6 literal используйте
квадратные скобки. Одинаковые включённые `host:port` отклоняются.

## Подключение

В корневом flake:

```nix
nixosConfigurations.my-node = mkNode {
  imports = [ ./nodes/my-node "${profiles}/rns-network/config.nix" ];
};
```

Профиль не добавляет входящих портов firewall. Он не выдаёт права remote shell: для этого
подключается `profiles/rnsh/config.nix` с явным `lattice.rnsh.allowed` и сохранением identity.
На homelab сохраняются `/var/lib/rns` и `/var/lib/rnsh` через `/persist`.

## Проверки и дальнейшее развитие

`checks.x86_64-linux.rns-network` проверяет два публичных uplink, замену реестра, отключение peer,
IPv6, отсутствие входящих портов, ошибочные настройки и реальный ConfigObj независимым parser.
Интернет-проверка выполняется отдельно на закреплённом `rns-rs`.

Автоматическое подключение обнаруженных интерфейсов и `bootstrap_only` пока не включены:
их поддержку в используемом Rust snapshot нужно проверить отдельно. Старый Amsterdam testnet
[закрыт](https://github.com/markqvist/Reticulum#public-testnet) и не используется.
