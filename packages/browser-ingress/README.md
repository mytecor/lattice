# Browser ingress

[Пакет](./package.nix) собирает Go-сервис `browser-ingress`, общий для
[Moli](./../moli/README.md) и [Foxbridge/Camoufox](./../../modules/services/foxbridge-camoufox/README.md).
Он слушает loopback за Caddy и Authentik.

Корневой адрес показывает страницу выбора без обращения к браузерам.
`/moli/` и необязательный `/foxbridge/` выбирают существующую вкладку через
discovery и перенаправляют на стандартный [DevTools frontend](./../devtools-frontend/README.md).
Статические ресурсы общие: `/devtools/frontend/`. CDP endpoints без префикса
возвращают 404.

[Ingress](./main.go) проксирует HTTP и WebSocket, разбирает JSON discovery
и переписывает только `webSocketDebuggerUrl` и `devtoolsFrontendUrl`, сохраняя
URL, заголовки и содержимое страниц. Публичные endpoint-ы сохраняют префикс
выбранного браузера, внешний host и схему `ws`/`wss`. Frontend и WebSocket
используют один origin и одну сессию Authentik.

Caddy обслуживает внешний HTTP/TLS ingress. Для CDP-запросов с Bearer-заголовком
он вызывает внутренний обработчик проверки ключей в `browser-ingress`. Запросы
с сессией Authentik проходят через ForwardAuth. Ключи для `browser-ingress`
загружаются через systemd, а Caddy удаляет заголовок `Authorization` при
проксировании проверенного запроса к браузеру.

## Запуск и проверки

```sh
browser-ingress --listen 127.0.0.1:9224 \
  --upstream http://127.0.0.1:9223 \
  --frontend /path/to/devtools-frontend \
  --foxbridge http://127.0.0.1:9222 \
  --idle-timeout 20m
```

`--frontend` обязателен. `--upstream` задаёт Moli, `--foxbridge` — необязательный
фиксированный upstream для `/foxbridge/`. Флаг `--idle-timeout` (по умолчанию 20m)
отслеживает активные WebSocket CDP-сессии к страницам; при отсутствии подключений
в течение таймаута непустые вкладки сбрасываются на `about:blank`, предотвращая фоновую
нагрузку на CPU и систему охлаждения. Привязка page-сессии и JSON text framing
Foxbridge обеспечиваются [патчем](./../foxbridge/devtools-page-socket.patch).

Флаг `--api-keys-file` включает проверку ключей на внутреннем маршруте `/_cdp_auth`.
Файл содержит один Bearer-ключ на строку и загружается при запуске.

[Go-тесты](./main_test.go) проверяют HTTP/HTTPS discovery, перенаправления,
статические ресурсы, маршрутизацию backend-ов, отсутствие старых endpoint-ов и сброс неактивных вкладок по таймауту.
[Runtime smoke](./../../tests/moli.nix) запускает настоящий Moli, проверяет
авторизацию discovery и WebSocket через Caddy, выполняет JavaScript и получает PNG.

Конфигурация сервиса и подключение описаны в [профиле](./../../profiles/moli/README.md).
