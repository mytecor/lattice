# Chrome DevTools frontend

[Пакет](./package.nix) содержит стандартный frontend из официального Chromium
snapshot `1684572` (Chromium 154). Архив закреплён SHA-256; браузер и Node.js
для обслуживания интерфейса не нужны. В Nix store устанавливаются готовые
HTML, JavaScript, CSS, изображения и прочие runtime-ресурсы без дубликатов
compression/hash и source maps.

[Ingress Moli](./../moli/README.md) обслуживает frontend под `/devtools/frontend/`
на том же origin, что и CDP WebSocket. Authentik защищает и статические ресурсы,
и соединение с браузером. Настройка описана в [профиле](./../../profiles/moli/README.md).

Upstream поддерживает
[hosted mode](https://chromium.googlesource.com/devtools/devtools-frontend/+/main/docs/get_the_code.md#Running-in-hosted-mode):
обычная web-страница inspector подключается к CDP endpoint через `ws`/`wss`.
