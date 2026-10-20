# Moli: удалённый CDP-браузер

[Профиль](./config.nix) запускает [Moli](./../../packages/moli/README.md)
с `serve --layout --resource`: доступны DOM, JavaScript, реальные координаты,
скриншоты и DevTools screencast. Это отдельный браузер; Jev продолжает использовать
[Foxbridge/Camoufox](./../browser-agent-stack/README.md).

[app-services](./../app-services/config.nix) публикует браузер через Caddy и mDNS:
`http://browser.mytecor-homelab.local/`. Корневой адрес перенаправляет на список
CDP targets `/json/list`; прежнего PhantomStream viewer больше нет.
Если mesh-сайт разрешён в [tcp-gateway](./../tcp-gateway/README.md), тот же CDP
доступен на `https://browser.<mesh-domain>/`.

Moli и ingress слушают только loopback, backend-порты закрыты firewall.
Ingress исправляет `webSocketDebuggerUrl` в discovery-ответах Moli: SDK получает
адрес внешнего сайта вместо loopback. Поле `devtoolsFrontendUrl` содержит только
идентификатор встроенного frontend Chrome без параметра `ws`: Chrome требует
его для штатного действия **inspect fallback** и сам подключает транспорт.
Moli не поставляет web frontend, а ручной URL встроенного Chrome DevTools
с удалённым WebSocket блокируется через CSP. Обычные данные страницы не переписываются.

Профиль требует Authentik и запись `service = "browser"` в `forwardAuth`.
Авторизация защищает discovery и WebSocket handshake. Сначала войдите через
`http://browser.mytecor-homelab.local/`; для SDK передавайте cookie этой
авторизованной сессии в заголовках подключения. Не сохраняйте cookie в Git
или логах. Пример Playwright (cookie приходит из окружения):

```javascript
import { chromium } from "playwright";

const browser = await chromium.connectOverCDP(
  "http://browser.mytecor-homelab.local",
  { headers: { Cookie: process.env.BROWSER_COOKIE } },
);
const context = browser.contexts()[0];
const page = context.pages()[0] ?? await context.newPage();
await page.goto("https://example.org");
console.log(await page.title());
```

## Подключение из Chrome

Используйте SSH-туннель и штатное обнаружение targets в Chrome. SSH авторизует
доступ к loopback ingress; cookie Authentik для этого пути не нужна.

На своей машине запустите и оставьте работающим:

```sh
ssh -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 \
  -L 127.0.0.1:19222:127.0.0.1:9224 root@mytecor-homelab.local
```

1. Откройте `chrome://inspect/#devices`.
2. Включите **Discover network targets**, нажмите **Configure…** и добавьте
   `127.0.0.1:19222`.
3. Под **Remote Target → 127.0.0.1:19222** найдите вкладку Moli и нажмите
   **inspect fallback**. Chrome откроет свой встроенный DevTools через нативное
   соединение; копировать `devtools://…?ws=…` вручную не нужно.
4. В Console можно выполнить `location.href = "https://example.org"` для перехода.

Остановить туннель можно через Ctrl+C в терминале. Локальный порт доступен только
на loopback вашей машины. Подключение через Chrome проверено с реальным Moli:
DevTools создаёт target и выполняет JavaScript.

Вкладки также создаются через CDP `Target.createTarget` или HTTP
`PUT /json/new?<url>`. Для SDK по внешнему сайту нужна cookie Authentik;
без неё сервер перенаправляет на вход вместо CDP JSON/upgrade.
Для машин вне LAN используйте mesh-имя: `.local` обнаруживается через mDNS
в локальной сети. Для SSH-туннеля в этом случае укажите доступное SSH-имя ноды.

Состояние Moli в этой конфигурации временное: cookies и вкладки браузера
не сохраняются после перезапуска сервиса.
