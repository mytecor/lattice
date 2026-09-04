# Профиль Reticulum

Подключается как `profiles/rns-server/config.nix` в составе ноды. Использует типизированный
[модуль rns-server](../../modules/rns-server/README.md) и общий реестр портов.

Для межузловой сети с общей точкой входа используйте вместо этого профиля
[`rns-network`](../rns-network/README.md). Этот профиль остаётся самостоятельным LAN/TCP-пресетом.

| Интерфейс | Дефолт |
| --------- | ------ |
| `Auto Discovery` | Включён; `AutoInterface`, scope `link`, порты 29716 и 42671 |
| `TCP Server` | Включён; `0.0.0.0:4242`, до 64 соединений |
| `TCP Uplink` | Отключён; `TCPClientInterface`, порт 4242, адрес задаёт нода |

Дефолты заданы через `lib.mkDefault` и переопределяются обычными значениями в конфигурации ноды.
TCP listener запускается вместе с профилем; для входящих подключений нужно явно открыть firewall:

```nix
lattice.rns-server.interfaces."TCP Server".openFirewall = true;
```

Для ноды, которая только подключается к выбранной точке входа:

```nix
lattice.rns-server.interfaces = {
  "TCP Server".enabled = false;
  "TCP Uplink" = {
    enabled = true;
    target_host = "entry.example.net"; # Заменить адресом своей точки входа.
    # target_port = 4242; уже задан профилем.
  };
};
```

Внешняя доступность сервера требует маршрута и, при необходимости, forwarding на маршрутизаторе.
Профиль не подключает ноду к чужой публичной сети и не выбирает публичный адрес Lattice:
это [f3-02](../../docs/roadmap/tasks/f3-02-define-entry-points.md). Для узла, который маршрутизирует
Reticulum-трафик между соседями, отдельно включается `reticulum.enable_transport = true`;
сам TCP listener не включает transport routing.

Профиль подключён к `nodes/example` корневым flake. `mytecor-homelab` пока использует только
базовый профиль; его подключение к межузловой сети выполняется вместе с выбранной топологией
и вторым узлом в F3. `config` генерируется декларативно: ручные правки `/var/lib/rns/config`
будут заменены при старте сервиса.
