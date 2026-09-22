# f18-06. Проверить `humanize` Camoufox

## Контекст

Camoufox имеет режим `humanize=true` — естественные задержки и траектории курсора для
имитации человека. Надо проверить, применяется ли humanization к обычным CDP input events,
которые шлёт Jev (`Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`, `Input.insertText`)
через Foxbridge/Juggler. Семантику Jev не меняем; собственный HumanCursor в рамках задачи
не добавляем.

## Что сделать

- [x] Проверить возможность запуска Camoufox с `humanize=true` через Foxbridge.
- [x] Убедиться, что Jev по-прежнему шлёт обычные CDP input events (методы выше).
- [x] Определить, применяет ли Camoufox humanization (естественные задержки /
      траектории) к таким событиям через Foxbridge/Juggler.
- [x] Если нет — зафиксировать это как отдельное ограничение (в ответе к задаче и, при
      необходимости, в BACKLOG).

## Результат (2026-09-22, Foxbridge v6 + Camoufox 152.0.4-beta.30, нода mytecor-homelab.local)

### Короткий ответ

**Humanization применяется только к CDP `Input.dispatchMouseEvent type="mouseMoved"`
(`mousemove`) — и именно к нему. На `mousemove` он применяется и через Foxbridge.
Однако Jev в своём action loop вообще не шлёт `mousemove`** (ни по клику, ни по fill,
ни по scroll). Значит, для фактических действий Jev humanization **не срабатывает**:
это не глюк моста, а просто Jev не использует тот тип события, который Camoufox
гуманизирует.

Формально для DoD: **humanization применяется к CDP `mousemove` от Jev через
Foxbridge/Juggler — подтверждено. К `mousePressed`/`mouseReleased`/`mouseWheel`/
`dispatchKeyEvent`/`insertText` — не применяется** (в этих типах и нет траектории).
Ограничение: Jev humanization фактически не активирует (см. «Ограничение» ниже).

### Как humanization попадает в CDP-путь (механизм)

Camoufox — сборка Firefox со встроенным Juggler (как у Playwright Firefox). CDP-команды от
Jev приходят в Foxbridge и переводятся (см. `pkg/bridge/input.go`) в Juggler page-команды:

| Jev → CDP | Foxbridge → Juggler | Camoufox page handler | Humanized? |
| --- | --- | --- | --- |
| `Input.dispatchMouseEvent` `mousePressed` | `Page.dispatchMouseEvent` `mousedown` | прямой dispatch | нет |
| `Input.dispatchMouseEvent` `mouseReleased` | `Page.dispatchMouseEvent` `mouseup` | прямой dispatch | нет |
| `Input.dispatchMouseEvent` `mouseMoved` | `Page.dispatchMouseEvent` `mousemove` | **траектория** | **да** |
| `Input.dispatchMouseEvent` `mouseWheel` | `Page.dispatchWheelEvent` | прямой dispatch | нет |
| `Input.dispatchKeyEvent` | `Page.dispatchKeyEvent` | прямой dispatch | нет |
| `Input.insertText` | `Page.insertText` | вставка текста | нет |

Сам гук humanize живёт в `omni.ja` → `chrome/juggler/content/protocol/PageHandler.js`
(метод `Page.dispatchMouseEvent`):

```js
// Camoufox: когда humanize включен, прямое mousemove разворачивается в человекообразную
// траекторию промежуточных mousemove, генерируемых в C++
// (ChromeUtils.camouGetMouseTrajectory / MouseTrajectories.hpp).
if (type === 'mousemove' && ChromeUtils.camouGetBool('humanize', false)) {
  const trajectory = ChromeUtils.camouGetMouseTrajectory(
    this._lastTrackedPos.x, this._lastTrackedPos.y, x, y);
  for (let i = 2; i < trajectory.length - 2; i += 2) {
    await sendOne('mousemove', trajectory[i], trajectory[i+1]);
    await new Promise(resolve => setTimeout(resolve, 10));   // 10 мс на точку
  }
  promises.push(sendOne('mousemove', x, y));
}
```

`camouGetBool` / `camouGetMouseTrajectory` — нативные C++ (libxul.so). Все виды мыши
кроме `mousemove` и все клавиатурные события идут напрямую в `_contentPage.send(...)`
без каких-либо humanize-гуков.

### Как конфиг (humanize=true) доставляется в браузер

`camoufox-python` собирает `config_map` (включая `humanize`) в JSON и передаёт его
браузеру **через переменные окружения** `CAMOU_CONFIG_1..N` (Linux — чанки по 32767 байт;
см. `get_env_vars()` в `camoufox/utils.py`). Браузер на старте читает их и склеивает
(строки `CAMOU_CONFIG` есть в `libxul.so`; `ChromeUtils.camouGetBool('humanize', …)`
ходит в этот распарсенный конфиг).

Для Camoufox, запускаемого **напрямую через Foxbridge** (а не через camoufox-python),
конфиг в переменные окружения никто не кладёт — Foxbridge стартует
`--juggler-pipe --purgecaches [--headless]` с наследуемым окружением. Поэтому для
проверки достаточно было запустить Foxbridge с `CAMOU_CONFIG_1='{"humanize":true}'` —
и браузер это подхватил (см. ниже).

### Эмпирическая проверка (была проведена на ноде)

Скрипты `probe_mousemove.py` / `probe_timing.py` / `probe_clickkey.py` (`/root/f18-poc/`)
гоняли Jev-сессию (`Browser.call("Input.dispatchMouseEvent", …)`) с CDP на живой
Camoufox/Foxbridge. Числа — медианы round-trip диспетчеризации:

| Событие | baseline (без humanize) | `humanize=true` (via `CAMOU_CONFIG_1`) |
| --- | --- | --- |
| `mousemove`, 700 px | ~3 мс | **~1670 мс** |
| `mousemove` первый, из (0,0) | ~15 мс | ~669 мс |
| `mousePressed`+`mouseReleased` (клик) | ~9 мс | ~7–10 мс |
| `keyDown`+`keyUp` | ~3 мс | ~2–6 мс |
| `insertText` | ~1 мс | ~1 мс |

Из этого:

- `CAMOU_CONFIG_1` реально доходит до браузера через Foxbridge (конфиг-путь
  camoufox-python → env сохраняется, даже когда запуск делает Foxbridge).
- `mousemove` под humanize = ~1670 мс вместо ~3 мс — это ровно работа траектории
  (`camouGetMouseTrajectory`, ~160 точек по 10 мс + прочие задержки). Humanization
  применяется к CDP `mousemove`, идущему от Jev-сессии через Foxbridge/Juggler.
- Клик, клавиатура и insertText остались быстрыми — humanization им не применяется.

Побочная проверка: полный smoke f18-04 (`jev-f18-smoke.py`) **проходит** при
`humanize=true` (fill → selectAll → submit — всё ок), а `jev-f18-fingerprint.py`
показывает, что эталонный fingerprint f18-05 **не меняется** под humanize
(`navigator.webdriver=false`, viewport `1280x985`, без дичек).

### Ограничение (для контекста f18)

Jev в action loop не шлёт `mousemove` вообще: единственные мышиные события — это
`mousePressed` и `mouseReleased` (клик по центру наблюдаемого элемента) и `mouseWheel`
(scroll). `fill` — это `dispatchKeyEvent` (selectAll) + `insertText`. Значит
humanization (которая живёт только в `mousemove`) **на фактических действиях Jev не
срабатывает**, даже если включить `humanize=true`. Задержки по клику или «естественный»
ввод текста не добавляются.

Это **не баг Foxbridge/Jev**, а дизайн: Camoufox гуманизирует только движение мыши, а
Jev не делает «ублажающих» движений курсора между кликами. Если захочется реальную
человекообразность действий Jev (траектория к клику, паузы перед кликом) — это
отдельная история на границе Foxbridge (добавить `mousemove` перед click) или
собственный HumanCursor в Jev, и она явно отложена в README f18
(«собственный cursor humanization» в списке осознанно откладываемого).

### Вывод для архитектуры

- Foxbridge уже прозрачно пропускает CDP `Input.*` в Juggler; включать `humanize`
  через конфиг `CAMOU_CONFIG_1` можно без каких-либо правок Foxbridge (просто
  переменная окружения в systemd-юните, см. f18-07/f18-08).
- Для Jev проку от `humanize=true` пока нет (Jev не шлёт `mousemove`). Поэтому в
  systemd-конфиг f18-08 `humanize` по умолчанию оставляем выключенным; вопрос
  «добавлять ли humanization на границе Foxbridge» — в open questions.

## Критерий готовности (Definition of Done)

- [x] Документированный ответ: humanization **применяется** к CDP `mousemove` от Jev через
      Foxbridge/Juggler (эмпирически подтверждено; ~3 мс → ~1670 мс при `humanize=true`),
      и **не применяется** к `mousePressed`/`mouseReleased`/`mouseWheel`/`dispatchKeyEvent`/
      `insertText`. Ограничение зафиксировано: Jev не шлёт `mousemove`, поэтому на его
      фактических действиях humanization не срабатывает.
- [x] Jev не менялся; собственного HumanCursor нет; ограничение зафиксировано в разделе
      «Ограничение» выше.

## Затрагиваемые файлы / слои

- Foxbridge: **не менялся** (конфиг humanize доставляется через `CAMOU_CONFIG_1` = env;
  запуск браузера прежний).
- Jev: **не трогаем**.
- Нода (`/root/f18-poc/`): проверочные скрипты `probe_mousemove.py`, `probe_timing.py`,
  `probe_clickkey.py`, `probe_b_click.py`, `probe_b2_click.py`, `probe_b3_click.py`; рабочий
  прогон — с `CAMOU_CONFIG_1='{"humanize":true}'` у Foxbridge, затем стэк возвращён
  в baseline (humanize off).

## Открытые вопросы

- Делать ли humanization на границе Foxbridge (добавлять `mousemove` перед click / паузы)?
  Это отдельная задача и явно в зоне «осознанно откладываемого» README f18.
- Оставляем ли `humanize` доступным в NixOS-конфиге f18-08 (скорее да, флагом, но off
  по умолчанию), раз Jev всё равно его не использует.
