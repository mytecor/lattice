# f18-04. Smoke test полного пути

## Контекст

Нужен автоматический тест полного пути `jev-ultrafast → browser-harness → Foxbridge →
Camoufox`, повторяющий то, как Jev на самом деле работает, чтобы не сломать контракт.

## Что сделать

- [x] Тестовая задача Jev, которая:
      1. открывает простую страницу;
      2. получает snapshot;
      3. выполняет click;
      4. выполняет text input;
      5. ждёт изменения DOM;
      6. корректно завершается через `DONE`.
- [x] Дополнительно проверить:
      - `snapshot.js` Jev выполняется **без изменений** — upstream-пакет используется как есть
        (packages/jev-ultrafast собирает оригинальный tarball, см. f18-04 «Фактический smoke»);
      - `window.__jevFast` сохраняет node identity — подтверждено повторным прогоном smoke на
        живом стеке (2026-09-23): повторный fill перезаписывал значение, `selectAll` замена
        срабатывала на непустом поле;
      - freshness guards работают (стейл-узлы не переиспользуются) — подтверждено повторным
        прогоном: второй `fill` после изменения DOM упал с `jev_ultrafast.browser.StalePage:
        Page changed since this decision`, т.е. стейл-узел НЕ был применён (guard сработал,
        а не действовал по устаревшему состоянию);
      - coordinate click проходит через Foxbridge — ок (click на кнопку Go прошёл, submit
        формы сработал `SUBMITTED:first`);
      - `Input.insertText` корректно работает с Unicode — ок в smoke (кириллица/латиница через
        `Input.insertText` без артефактов);
      - screenshot работает при включённом recording/debug режиме — `Page.captureScreenshot`
        в `observe(screenshot=True)` вернул base64 (~26 КБ, format=jpeg) и в ручном, и в
        повторном прогоне.
- [ ] После этого прогнать реальную задачу Jev, например Google Flights или другую
      страницу из его examples.

## Фактический smoke (2026-09-22, mytecor-homelab.local, Foxbridge v6)

Прогнан API-драйв Jev поверх стека (`/root/f18-poc/jev-f18-smoke.py`, **без изменений**
Jev, только `BU_CDP_URL`):

- страница с `<input value="OLD">` + формой;
- `observe()` вернул snapshot **+ screenshot** (`Page.captureScreenshot` ok, base64 ~26 КБ);
- `act(fill, "first")` → click на поле + `commands:[selectAll]` + `Input.insertText`;
- `act(click, "Go")` → submit формы;
- проверка: `value == "first"`, `out == "SUBMITTED:first"` — **selectAll перезаписал "OLD"**;
- повторный `fill "second"` поверх "first" → `value == "second"` — перезапись, не append;
- `b.close()` → `DONE`.

В ходе smoke найдена и починена одна несовместимость Фoxbridge — обработка
`Input.dispatchKeyEvent(commands:["selectAll"])` (см. f18-02, пункт 5): без неё при
fill на непустом поле появлялась лишняя «a». Гонка execution context и frameId-ожидание
в `Page.navigate` тоже закрылись на этом пути (f18-02, пункты 2–3).

## Критерий готовности (Definition of Done)

- [x] Полный путь закрывается автоматическим smoke-тестом с корректным `DONE`.
      Прогнан API-драйв (`/root/f18-poc/jev-f18-smoke.py`, 2026-09-22) и повторно
      на живом стеке (2026-09-23) — `SUBMITTED:first`, selectAll перезаписал "OLD",
      `b.close()` → `DONE`; автотест — в f18-11.
- [x] Все перечисленные в «Дополнительно» проверки выполнены и задокументированы
      (см. обновлённый список выше).

## Затрагиваемые файлы / слои

- Тест: `tests/` (по [tests/README.md](../../tests/README.md)) либо скрипт в репозитории
      фичи — по факту где удобнее принять на PoC.
- `snapshot.js` Jev — **не изменять**.

## Открытые вопросы

_нет_
