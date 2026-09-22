# f18-02. Проверить CDP surface, необходимый Jev

## Контекст

Jev общается с браузером через `browser-harness` по CDP. Нужно убедиться, что Foxbridge
реально реализует ровно те CDP-методы, которые вызывает текущий `jev_ultrafast/browser.py`.
Задача — не «сделать абстрактный compatibility layer заранее», а зафиксировать только
**реально найденные** несовместимости.

## Что сделать

- [ ] Открыть `jev_ultrafast/browser.py` и выписать точный список вызываемых CDP-методов.
- [ ] Проверить реальным запуском поддержку каждого из:
      `Target.createTarget`, `Target.attachToTarget`, `Target.closeTarget`,
      `Runtime.evaluate`, `Page.navigate`, `Page.captureScreenshot`,
      `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`, `Input.insertText`,
      `Emulation.setDeviceMetricsOverride`, `Emulation.setFocusEmulationEnabled`.
- [ ] Особо проверить `Runtime.evaluate(awaitPromise=true)` — Jev использует promise-based
      ожидание после input (resolution — это деталь реализации, важен сам вызов).
- [ ] Для каждого найденного расхождения записать: метод, что ожидает Jev, что отдаёт
      Foxbridge, вариант исправления (где — предпочтительно в Foxbridge).
- [ ] Зафиксировать результат в ответе к задаче (таблица «метод → OK/несовместимость →
      где чинить»).

## Критерий готовности (Definition of Done)

- [ ] Есть воспроизводимый чек-лист всех перечисленных методов с фактической поддержкой
      через Foxbridge/Camoufox.
- [ ] Ни одна несовместимость не «запланирована на будущее»: каждая либо подтверждена
      рабочим вызовом, либо заведена отдельной задачей починки.

## Затрагиваемые файлы / слои

- Читаем: node_modules Jev (`*_ultrafast/browser.py`) — только чтение.
- Результат идёт в f18-03 (focus emulation), f18-04 (smoke), f18-05 (viewport/fingerprint).

## Открытые вопросы

_нет_
