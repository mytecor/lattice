# Pi ACP daemon module

Модуль `lattice.pi-acp-daemon` запускает закреплённый Hydra ACP как foreground systemd service.
Hydra слушает только loopback, хранит session metadata в `StateDirectory` и создаёт каждую сессию
через локальный [`pi-acp`](../../packages/pi-acp/README.md). Плавающий ACP Registry для Pi не
используется.

Текущий LAN endpoint намеренно не имеет authentication. Опция `internalToken` — публичная
фиксированная строка для обязательного Caddy-to-Hydra handshake, а не credential или граница
доступа. Hydra получает её через файл в `RuntimeDirectory`; Caddy добавляет строку только к
внутреннему loopback query.

Внешний LAN endpoint — `ws://acp.<nodename>.local/`. Обязательный для Hydra путь `/acp` остаётся
внутренней деталью loopback upstream и добавляется Caddy через rewrite.

## Форма соединения клиента

Единственная проверенная форма клиентского подключения — чистый ACP WebSocket с subprotocol
`acp.v1` и без token/path/URL сессии (сессия выбирается внутри ACP). Так подключается Ferngeist;
эту же форму соединения покрывает `tests/acp-ingress-smoke.mjs`.

Stock-клиент `hydra-acp` (`acp`/`shim`/`cat`) для этого endpoint **не подходит**: он требует
login-credential для не-loopback хоста, который без master password у daemon получить нельзя, а
Caddy-rewrite любого path во внутренний `/acp` делает HTTP API клиента недостижимым. Задача на
auth-границу и stdio shim для Zed вынесена в [BACKLOG.md](../../docs/roadmap/BACKLOG.md).

Endpoint нельзя публиковать за пределами доверенной LAN: любой клиент с сетевым доступом может
создать Pi-сессию и использовать доступные ей shell/tools с правами пользователя сервиса.

Профиль ingress находится в
[`profiles/tcp-gateway`](../../profiles/tcp-gateway/README.md), а задача и acceptance-контракт — в
[f8-06](../../docs/roadmap/f8-pi-runtime/f8-06-network-acp-daemon.md).
