# f18-05. Проверить, что Foxbridge не ломает fingerprint Camoufox

## Контекст

Camoufox даёт настроенный антидетект-fingerprint. CDP-команды от Jev не должны случайно
перетирать его. Особое внимание — `Emulation.setDeviceMetricsOverride`: Jev выставляет
viewport `1120x780`, `deviceScaleFactor=1`, `mobile=false`; надо проверить, не создаёт ли
это несогласованный fingerprint.

## Что сделать

- [ ] Проверить на работающем стеке хотя бы: `navigator.webdriver`, user agent, platform,
      timezone, locale, WebGL, canvas, screen/viewport, `hardwareConcurrency`.
- [ ] Прогнать проверку с вызовом `Emulation.setDeviceMetricsOverride` (в точности как Jev)
      и без него; сравнить fingerprint.
- [ ] Если `setDeviceMetricsOverride` создаёт несогласованность (viewpoort vs
      `window.innerWidth`, разрешение vs CSS-пиксели, `deviceScaleFactor`), исправлять
      предпочтительно на границе Foxbridge/Camoufox, а не внутри Jev policy.
- [ ] Зафиксировать полный полученный fingerprint как эталон для последующих сравнений.

## Критерий готовности (Definition of Done)

- [ ] Fingerprint Camoufox сохранён: CDP-команды Jev не перетирают перечисленные свойства.
- [ ] Если исправление требовалось — оно сделано на границе Foxbridge/Camoufox.

## Затрагиваемые файлы / слои

- Foxbridge (обработка/нормирование `setDeviceMetricsOverride`).
- Jev **не трогаем**.

## Открытые вопросы

- Что считается «несогласованным»: расхождение `screen`/`innerWidth` после
  `setDeviceMetricsOverride` или что-то ещё — уточняется по фактическому результату.
