# Moli

[Пакет](./package.nix) закрепляет Linux x86_64 release `1.1.18`
[Moli](https://github.com/lexmount/moli), проверяет SHA-256 архива, адаптирует
ELF-зависимости к NixOS и задаёт системный CA bundle. Fontconfig и zlib входят
в runtime closure. Проверка установки запускает `--version` и `serve --help`.

[Ingress](./ingress/main.go) — стандартный Go reverse proxy с поддержкой WebSocket.
Он переписывает `webSocketDebuggerUrl` в CDP discovery и задаёт
`devtoolsFrontendUrl` как идентификатор встроенного inspector Chrome без `ws`,
сохраняя URL и содержимое страниц. [Тесты](./ingress/main_test.go) проверяют HTTP/HTTPS discovery и передачу
запросов; [runtime smoke](./../../tests/moli.nix) запускает настоящий Moli,
подключается по WebSocket, выполняет JavaScript и получает PNG.

Развёртывание и подключение описаны в [профиле](./../../profiles/moli/README.md).
