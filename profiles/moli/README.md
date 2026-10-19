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
Ingress исправляет `webSocketDebuggerUrl` и параметр `ws`/`wss` в
`devtoolsFrontendUrl` в discovery-ответах Moli: клиент получает адрес внешнего
сайта вместо loopback. Для frontend используется встроенный inspector Chrome
(`devtools://devtools/bundled/inspector.html`), поскольку Moli не поставляет web UI. Обычные данные страницы не переписываются.

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

Для Chrome DevTools после входа откройте `devtoolsFrontendUrl` нужной вкладки
из `/json/list` в том же Chrome-профиле. Вкладки создаются через CDP
`Target.createTarget` или HTTP `PUT /json/new?<url>` с той же cookie.
Клиенты без cookie получат перенаправление на вход вместо CDP JSON/upgrade.
Для машин вне LAN используйте mesh-имя: `.local` обнаруживается через mDNS
в локальной сети.

Состояние Moli в этой конфигурации временное: cookies и вкладки браузера
не сохраняются после перезапуска сервиса.
