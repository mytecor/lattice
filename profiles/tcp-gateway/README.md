# TCP gateway

Профиль включает Caddy и публикует активные HTTP-сервисы ноды. Внешние listeners принадлежат
только gateway; backend-сервисы должны слушать loopback.

При явно заданном `networking.domain` Caddy использует обычные hostname site labels и управляет
HTTPS автоматически. Без домена профиль использует внутренний суффикс `lattice` и явно создаёт
HTTP site labels, чтобы Caddy не запрашивал публичные сертификаты для несуществующего TLD.

Сейчас профиль автоматически добавляет reverse-proxy routes для `radicle-httpd` и HTTP control
plane `rns-server`. Прикладной node-status route, обслуживаемый самим Caddy, добавляет
[`profiles/app-services`](../app-services/README.md).
