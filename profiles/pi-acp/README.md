# Pi ACP profile

Профиль включает [`lattice.pi-acp-daemon`](../../modules/pi-acp-daemon/README.md) на общем
внутреннем порту из [`profiles/networking/ports.nix`](../networking/ports.nix). Сам Hydra listener
остаётся loopback-only; WebSocket ingress и mDNS alias добавляет
[`tcp-gateway`](../tcp-gateway/README.md).

Канонический клиентский endpoint — `ws://acp.<nodename>.local/`; отдельного path в конфигурации
клиента нет. Клиент подключается чистым ACP WebSocket (`acp.v1`), сессия выбирается внутри ACP
(`session/new` / `session/attach`). Проверенный сценарий такого клиента — Ferngeist; запись о
несовместимости stock `hydra-acp` client и отложенном Zed-шлюзе — в
[документации модуля](../../modules/pi-acp-daemon/README.md) и
[BACKLOG](../../docs/roadmap/BACKLOG.md).

Текущий endpoint работает без auth и предназначен только для доверенной LAN. Граница риска и
внутренний handshake описаны в
[документации модуля](../../modules/pi-acp-daemon/README.md).
