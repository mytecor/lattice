# f18-02. Проверить CDP surface, необходимый Jev

## Контекст

Jev общается с браузером через `browser-harness` по CDP. Нужно убедиться, что Foxbridge
реально реализует ровно те CDP-методы, которые вызывает текущий `jev_ultrafast/browser.py`.
Задача — не «сделать абстрактный compatibility layer заранее», а зафиксировать только
**реально найденные** несовместимости.

## Фактический CDP surface Jev (подтверждён запуском 2026-09-22)

Список методов снят с реального лога Foxbridge (v6, полный smoke): все методы, что Jev
фактически посылает, прошли.

| Метод | Jev ожидает | Foxbridge | Вердикт |
| --- | --- | --- | --- |
| `Target.getTargets` | список таргетов | OK | рабочее |
| `Target.createTarget` | новый page-таргет | OK (page via Juggler) | рабочее |
| `Target.attachToTarget` | sessionId | OK | рабочее |
| `Target.closeTarget` | закрыть таргет | OK | рабочее |
| `Page.enable` | ок | OK | рабочее |
| `Page.navigate` | frameId + url | OK — требуется frameId (см. патч) | рабочее после патча |
| `Page.captureScreenshot` | JPEG data | OK (format=jpeg) | рабочее |
| `Runtime.enable` | ок | OK | рабочее |
| `Runtime.evaluate` | результат | OK — нужен retry на гонке контекстов | рабочее после патча |
| `Runtime.evaluate(awaitPromise=true)` | promise результат | OK (after_input path) | рабочее |
| `DOM.enable` | ок | OK | рабочее |
| `Network.enable` | ок | OK | рабочее |
| `Emulation.setDeviceMetricsOverride` | viewport | OK → `Browser.setDefaultViewport` | рабочее |
| `Emulation.setFocusEmulationEnabled` | success no-op | OK (no-op) | рабочее — см. f18-03 |
| `Input.dispatchMouseEvent` | click | OK (mousedown/mouseup/wheel) | рабочее |
| `Input.dispatchKeyEvent` | key events | OK — `commands:[selectAll]` обрабатывается (см. ниже) | рабочее после патча |
| `Input.insertText` | ввод текста | OK (`Page.insertText`) | рабочее |

## Реально найденные несовместимости (и где починено)

Все подтверждены рабочим вызовом на v6:

1. **`Emulation.setFocusEmulationEnabled` не реализован** в Foxbridge v0.1.1 (возвращал
   `-32601 method not found`). Решение — no-op в Foxbridge → f18-03. Jev не тронут.
2. **`Page.navigate` до появления frame**: Jev создаёт фоновый `about:blank` таргет и
   вызывает `Page.navigate` сразу после `Target.attachToTarget`, до того как Juggler
   отдал frame. В v0.1.1 Foxbridge звал Juggler без `frameId` → падал required-field
   error. Починено ожиданием frame (≤2 с) в `page.go`.
3. **Гонка execution context при навигации**: Juggler быстро уничтожает контексты;
   Jev делает eager `Runtime.evaluate` после навигации → `Failed to find execution
   context`. Починено повторной выборкой последнего контекста и retry (≤3 попыток)
   в `runtime.go`.
4. **Frame-ID translation**: Juggler и CDP используют разные frame id по жизни навигации;
   в v3 добавлены `jugglerFrameIDForSession`/`cdpFrameIDForSession` для согласования
   `Page.navigate` и lifecycle-событий, а также `normalizeRuntimeResult` для формы ответа
   `Runtime.evaluate`.
5. **`Input.dispatchKeyEvent` `commands:[selectAll]`**: Jev при fill шлёт Ctrl+A
   (`commands:["selectAll"]` + `modifiers=2`), чтобы перезаписать существующий текст.
   Старый Foxbridge отбрасывал `commands` и слал голый key `a` → в поле появлялась
   лишняя «a» при очистке непустого поля. Juggler не умеет modifiers/commands для key
   (в протоколе `dispatchKeyEvent` их нет). Решение — перехват `commands:[selectAll]`
   в Foxbridge и нативный select-all активного элемента через `Runtime.evaluate`
   (с `executionContextId` из последнего контекста сессии). Jev не тронут.

   Суть патча в `pkg/bridge/input.go`
   (`Input.dispatchKeyEvent`, ключевой случай):

   ```go
   // CDP editor commands (e.g. ["selectAll"] from Jev's fill path).
   // Juggler's Page.dispatchKeyEvent has no modifiers/commands concept, so we
   // execute the editor command natively via Runtime.evaluate.
   if len(params.Commands) > 0 && params.Type == "keyDown" {
       switch strings.Join(params.Commands, ",") {
       case "selectAll":
           execCtxID := b.latestContextForSession(msg.SessionID)
           if execCtxID == "" {
               return json.RawMessage(`{}`), nil
           }
           b.callJuggler(msg.SessionID, "Runtime.evaluate", map[string]interface{}{
               "expression": `(() => {
                   const el = document.activeElement;
                   if (!el || !el.select) return;
                   el.select(); el.focus();
               })()`,
               "executionContextId": execCtxID,
               "returnByValue":     true,
           })
           return json.RawMessage(`{}`), nil
       }
   }
   ```

   Наивная передача `modifiers` в `Page.dispatchKeyEvent` не работает — Juggler
   отвергает поле (`Found property "<root>.modifiers" which is not described in this
   scheme`), поэтому select-all выполняется нативно, а не синтезом горячей клавиши.

## Что сделать

- [x] Открыть `jev_ultrafast/browser.py` и выписать точный список вызываемых CDP-методов.
- [x] Проверить реальным запуском поддержку каждого из:
      `Target.createTarget`, `Target.attachToTarget`, `Target.closeTarget`,
      `Runtime.evaluate`, `Page.navigate`, `Page.captureScreenshot`,
      `Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`, `Input.insertText`,
      `Emulation.setDeviceMetricsOverride`, `Emulation.setFocusEmulationEnabled`.
- [x] Особо проверить `Runtime.evaluate(awaitPromise=true)` — Jev использует promise-based
      ожидание после input (resolution — это деталь реализации, важен сам вызов).
- [x] Для каждого найденного расхождения записать: метод, что ожидает Jev, что отдаёт
      Foxbridge, вариант исправления (где — предпочтительно в Foxbridge).
- [x] Зафиксировать результат в ответе к задаче (таблица «метод → OK/несовместимость →
      где чинить»).

## Критерий готовности (Definition of Done)

- [x] Есть воспроизводимый чек-лист всех перечисленных методов с фактической поддержкой
      через Foxbridge/Camoufox.
- [x] Ни одна несовместимость не «запланирована на будущее»: каждая либо подтверждена
      рабочим вызовом, либо заведена отдельной задачей починки.

## Затрагиваемые файлы / слои

- Читаем: node_modules Jev (`*_ultrafast/browser.py`) — только чтение.
- Результат идёт в f18-03 (focus emulation), f18-04 (smoke), f18-05 (viewport/fingerprint).

## Открытые вопросы

_нет_
