# Browser stream

[Профиль](./config.nix) запускает [browser-stream](./../../packages/browser-stream/README.md)
как отдельный непривилегированный systemd-сервис. Он подключается к CDP существующего
[Foxbridge/Camoufox](./../browser-agent-stack/README.md) и слушает только loopback.

[app-services](./../app-services/config.nix) публикует `http://browser.<node>.local/` через
Caddy, регистрирует mDNS-алиас `browser` на LAN-аплинках и создаёт mesh-сайт, если он разрешён
в настройках [tcp-gateway](./../tcp-gateway/README.md). Backend-порт в firewall не открывается.

Профиль требует включённого Authentik и запись `service = "browser"` в `forwardAuth`.
Один ForwardAuth gate защищает и страницу, и все HTTP-запросы SDK, и WebSocket handshake.
Authentik создаёт LAN/mesh providers и карточки автоматически. WebSocket дополнительно
проверяет Origin; браузер не может подменить CDP endpoint через query-параметры.

Пример ссылки на homelab:

```text
http://browser.mytecor-homelab.local/?url=https%3A%2F%2Fexample.org%2F
```
