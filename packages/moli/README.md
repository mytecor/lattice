# Moli

[Пакет](./package.nix) закрепляет Linux x86_64 release `1.1.18`
[Moli](https://github.com/lexmount/moli), проверяет SHA-256 архива, адаптирует
ELF-зависимости к NixOS и задаёт системный CA bundle. Fontconfig и zlib входят
в runtime closure. Проверка установки запускает `--version` и `serve --help`.

[Ingress](./ingress/main.go) отдаёт стандартный
[DevTools frontend](./../devtools-frontend/README.md) и проксирует CDP WebSocket.
Он переписывает `webSocketDebuggerUrl` и `devtoolsFrontendUrl` в discovery,
сохраняя URL и содержимое страниц. Корневой адрес выбирает существующий page
через discovery и перенаправляет на inspector с WebSocket того же сайта.
[Тесты](./ingress/main_test.go) проверяют HTTP/HTTPS discovery, перенаправление,
статические ресурсы и передачу запросов; [runtime smoke](./../../tests/moli.nix)
запускает настоящий Moli, загружает frontend, подключается по WebSocket,
выполняет JavaScript и получает PNG.

Развёртывание и подключение описаны в [профиле](./../../profiles/moli/README.md).
