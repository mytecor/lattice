# Moli: удалённый CDP-браузер

[Профиль](./config.nix) запускает [Moli](./../../packages/moli/README.md)
с `serve --layout --resource`: доступны DOM, JavaScript, реальные координаты,
скриншоты и DevTools screencast. Это отдельный браузер; Jev продолжает использовать
[Foxbridge/Camoufox](./../browser-agent-stack/README.md).

[app-services](./../app-services/config.nix) публикует браузер через Caddy и mDNS:
`http://browser.mytecor-homelab.local/`. Корневой адрес показывает выбор браузера без подключения к вкладке.
Маршрут `/moli/` открывает стандартный
[Chrome DevTools frontend](./../../packages/devtools-frontend/README.md) для
первой вкладки Moli. Это обычная web-страница на том же домене, что и CDP:
SSH-туннель, `chrome://inspect` и ручные `devtools://` ссылки не требуются.
Если mesh-сайт разрешён в [tcp-gateway](./../tcp-gateway/README.md), интерфейс
и CDP также доступны на `https://browser.<mesh-domain>/`.

Moli и общий [browser-ingress](./../../packages/browser-ingress/README.md) слушают только loopback, backend-порты закрыты firewall.
Ingress отдаёт готовые статические ресурсы DevTools под `/devtools/frontend/`,
проксирует CDP и исправляет `webSocketDebuggerUrl` и `devtoolsFrontendUrl`
в discovery. HTTP-сайт использует `ws`, HTTPS-сайт — `wss`; frontend и WebSocket
остаются на одном origin и используют одну сессию Authentik.
Обычные данные страницы не переписываются.

Профиль требует Authentik и запись `service = "browser"` в `forwardAuth`.
Авторизация защищает frontend, discovery и WebSocket handshake. Сначала войдите через
`http://browser.mytecor-homelab.local/`; для SDK передавайте cookie этой
авторизованной сессии в заголовках подключения. Не сохраняйте cookie в Git
или логах. Пример Playwright (cookie приходит из окружения):

```javascript
import { chromium } from "playwright";

const browser = await chromium.connectOverCDP(
  "http://browser.mytecor-homelab.local/moli",
  { headers: { Cookie: process.env.BROWSER_COOKIE } },
);
const context = browser.contexts()[0];
const page = context.pages()[0] ?? await context.newPage();
await page.goto("https://example.org");
console.log(await page.title());
```

## Подключение из Chrome

1. Откройте `http://browser.mytecor-homelab.local/` и войдите через Authentik.
   Выберите Moli или перейдите сразу на `/moli/`.
2. Стандартный DevTools откроется для первой вкладки Moli.
3. В Console можно выполнить `location.href = "https://example.org"` для перехода.
   Для просмотра рендеринга используйте встроенный screencast DevTools.

Список всех вкладок Moli доступен на `/moli/json/list`. Поле `devtoolsFrontendUrl`
содержит относительную ссылку на inspector конкретной вкладки; открывайте её
на том же сайте. Для SDK доступны обычные CDP discovery и WebSocket endpoints.
Вкладки создаются через CDP `Target.createTarget` или HTTP `PUT /moli/json/new?<url>`.
Клиенты SDK по внешнему сайту передают cookie Authentik; без неё сервер
перенаправляет на вход вместо CDP JSON/upgrade.
Для машин вне LAN используйте mesh-имя: `.local` обнаруживается через mDNS
в локальной сети.

## Тест Foxbridge / Camoufox

При включённом [браузерном стеке](./../browser-agent-stack/README.md) доступен
`http://browser.mytecor-homelab.local/foxbridge/`. Он открывает тот же стандартный
frontend для первой вкладки Camoufox. Moli доступен через `/moli/`.
Оба маршрута защищены Authentik; отдельный порт снаружи не открывается.

Discovery Moli доступен под `/moli/json/list`, WebSocket — под
`/moli/devtools/page/<target-id>`. Адрес `/moli` перенаправляет на `/moli/`,
как `/foxbridge` на `/foxbridge/`. CDP endpoints без префикса недоступны.

Discovery Foxbridge доступен под `/foxbridge/json/list`, WebSocket — под
`/foxbridge/devtools/page/<target-id>`. Префикс сохраняется в выдаваемых ссылках,
поэтому клиент остаётся на выбранном backend. В frontend используется общий
путь `/devtools/frontend/`.

Это тест совместимости: Runtime и скриншоты поддерживаются Foxbridge,
но Elements получает упрощённый DOM, а остальные панели зависят от покрытия
CDP в закреплённой версии. Тест использует существующий Camoufox агента Jev:
навигация из DevTools меняет эту же вкладку. Не перезагружайте и не закрывайте
рабочие вкладки агента ради теста.

Состояние Moli в этой конфигурации временное: cookies и вкладки браузера
не сохраняются после перезапуска сервиса.
