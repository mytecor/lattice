# f18-05. Проверить, что Foxbridge не ломает fingerprint Camoufox

## Контекст

Camoufox даёт настроенный антидетект-fingerprint. CDP-команды от Jev не должны случайно
перетирать его. Особое внимание — `Emulation.setDeviceMetricsOverride`: Jev выставляет
viewport `1120x780`, `deviceScaleFactor=1`, `mobile=false`; надо проверить, не создаёт ли
это несогласованный fingerprint.

## Что сделать

- [x] Проверить на работающем стеке хотя бы: `navigator.webdriver`, user agent, platform,
      timezone, locale, WebGL, canvas, screen/viewport, `hardwareConcurrency`.
- [x] Прогнать проверку с вызовом `Emulation.setDeviceMetricsOverride` (в точности как Jev)
      и без него; сравнить fingerprint.
- [x] Если `setDeviceMetricsOverride` создаёт несогласованность (viewpoort vs
      `window.innerWidth`, разрешение vs CSS-пиксели, `deviceScaleFactor`), исправлять
      предпочтительно на границе Foxbridge/Camoufox, а не внутри Jev policy.
- [x] Зафиксировать полный полученный fingerprint как эталон для последующих сравнений.

## Результат (2026-09-22, Foxbridge v6 + Camoufox 152.0.4-beta.30, нода mytecor-homelab.local)

### Метод проверки

Скрипт `jev-f18-fingerprint.py` на ноде (`/root/f18-poc/`):
1. Снимает fingerprint по CDP без каких-либо emulation-команд.
2. Снимает тот же fingerprint после `Emulation.setDeviceMetricsOverride(1120, 780,
   deviceScaleFactor=1, mobile=false)` — ровно как это делает `Browser.__init__` в Jev.
3. Сравнивает два JSON-объекта на расхождение.

### Ключевой вывод: fingerprint сохранён полностью

Два прогона (без override и с override) дали **идентичный fingerprint** — по всем
перечисленным свойствам расхождения нет. Jev/CDP-команды не перетирают ни одно
антидетект-свойство Camoufox.

| Свойство | Значение (с override и без — одинаково) |
| --- | --- |
| `navigator.webdriver` | `false` |
| `navigator.userAgent` | `Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Camoufox/152.0.4-beta.30` |
| `navigator.platform` | `Linux x86_64` |
| `navigator.language` / `languages` | `en-US` / `["en-US","en"]` |
| `Intl.timeZone` / offset | `UTC` / `0` |
| `navigator.hardwareConcurrency` | `4` |
| `navigator.deviceMemory` | `null` (Camoufox не отдаёт) |
| `screen` | `1366x768`, `avail` `1366x768`, `colorDepth/pixelDepth` `24` |
| `inner` (window) | `1280x985` |
| `outer` (window) | `1280x1040` |
| `devicePixelRatio` | `1` |
| WebGL/WebGL2 | `false` (см. ниже) |
| canvas hash | стабильный (`iVBORw0KGgo...`) — одинаков без/с override |
| `domAutomationController` | `false` |
| `maxTouchPoints` | `0` |

Это и есть **эталонный fingerprint** f18-05 для последующих сравнений (f18-06+).

### Про `Emulation.setDeviceMetricsOverride` — важное уточнение

`setDeviceMetricsOverride(1120x780)` в текущем Foxbridge (v6) — **фактически no-op** для
уже открытой Jev-страницы: Foxbridge маппит его на Juggler `Browser.setDefaultViewport`
(браузерный уровень, вызов с `ignore error`), а это применяется только к *вновь создаваемым*
браузерным контекстам, не к таргету, который Jev создал заранее.

Проверено напрямую:
- target, созданный **до** override → `innerWidth/innerHeight` = `1280/985`;
- target, созданный **после** override → тоже `1280/985`.

То есть override **не меняет** фактическое разрешение — внутренние проверки Jev
(`snapshot.js`) работают честно на реальном `1280x985`, никакой несогласованности
`screen/innerWidth/DSF` не возникает. Fingerprint не «натягивается» на чужие значения.

**Следствие для архитектуры:** no-op здесь — это не баг, а благо (не создаёт расхождения).
Если в будущем понадобится, чтобы Jev реально работал в `1120x780`, это отдельная задача
(ожидание viewport или отдельный прогон override до создания таргета) — **вне** f18-05
и вне критерия «не менять Jev».

### WebGL: недоступен, но это константа окружения, а не сброс fingerprint

`canvas.getContext('webgl')` и `webgl2` — `false`/`null` в обоих прогонах, т.е. это не
следствие override/CDP и не «перетирание» fingerprint. Это свойство headless-окружения
ноды (GL-либы в nix-store есть, но SwiftShader-рендер не поднят / блоклист WebGL в
headless). `renderer/vendor` — `null`.

Для заданного критерия (Jev/CDP не перетирают свойства) это некритично: значение
**стабильно** и не меняется от команд Jev. Если станет важно иметь ненулевой WebGL
fingerprint (например, для задач с пуленепробиваемым антидетектом) — это отдельная
задача на уровне запуска Camoufox/Foxbridge, не Jev.

## Критерий готовности (Definition of Done)

- [x] Fingerprint Camoufox сохранён: CDP-команды Jev не перетирают перечисленные свойства.
      Полный fingerprint воспроизводим и эталонный (см. таблицу).
- [x] Исправление не потребовалось, потому что `setDeviceMetricsOverride` не создаёт
      несогласованности (он no-op на уровне страницы — см. выше). Граница
      Foxbridge/Camoufox не менялась.

## Затрагиваемые файлы / слои

- Foxbridge (обработка/нормирование `setDeviceMetricsOverride`) — **не менялся** в этой
  задаче: поведение описано, а не исправлено (ничего чинить не нужно).
- Jev **не трогаем**.
- Нода: `/root/f18-poc/jev-f18-fingerprint.py` (проверочный скрипт), результат выше.

## Открытые вопросы

- Нужен ли WebGL как отдельная задача? (Текущий headless-Camoufox отдаёт WebGL off
  стабильно; для горизонта «реальные web-задачи» обычно достаточно. Решается при
  f18-11, если задача Jev упрётся в WebGL-детект.)
- Нужен ли апгрейд `setDeviceMetricsOverride` до «включить 1120x780»? Вне критерия
  f18-05; Jev работает на реальном `1280x985`. Отметка на будущее.
