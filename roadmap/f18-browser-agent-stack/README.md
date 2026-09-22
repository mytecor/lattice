# F18. Браузерный стек для агентов (Jev + Camoufox через Foxbridge)

Развернуть в homelab отдельный браузерный runtime для агентов: оригинальный
`browser-use/jev-ultrafast` (decision loop + snapshot) управляет браузером
**Camoufox** (антидетект, Firefox-based) через **Foxbridge** — CDP compatibility layer,
который переводит CDP-команды от `browser-harness` в Juggler-команды для Camoufox.
Jev не знает ничего про Camoufox/Foxbridge: всё связывает `BU_CDP_URL`.

Соответствует новой вехе в [ROADMAP.md](../../ROADMAP.md#f18-браузерный-стек-для-агентов).

## Зачем

- У агентов (Pi) сейчас нет браузерного инструмента. Чтобы закрывать реальные web-задачи
  (Google Flights и т.п.), нужен рабочий стек: агент → Jev → browser-harness → CDP → браузер.
- Camoufox — антидетект-сборка Firefox с firmer fingerprint; его нельзя заменить на чистый
  Chromium без потери stealth-свойств. Foxbridge (Node) умеет гонять Camoufox через Juggler
  и отдаёт наружу CDP-интерфейс — то, что понимает `browser-harness` из Jev.
- Homelab на NixOS: итоговая конфигурация обязана быть декларативной и подниматься
  через systemd, без ручных шагов после `nixos-rebuild switch`.

## Ключевое требование

**Не менять Jev policy и agent loop.** Используем оригинальный `browser-use/jev-ultrafast`
максимально без изменений — без локального форка, собственной policy, замены `snapshot.js`
и переноса на Playwright. Все Firefox/Camoufox-specific вычисления живут в Foxbridge.

## Архитектура

```text
ACP client
    ↓
Pi
    ↓ browser_task(url, goal)
Jev wrapper
    ↓
browser-use/jev-ultrafast
    ↓
browser-harness
    ↓ BU_CDP_URL
Foxbridge
    ↓ Juggler
Camoufox
    ↓
Web
```

Два отдельных systemd-сервиса: `foxbridge-camoufox.service` (long-running browser runtime)
и `jev-ultrafast.service` (агент, зависит от первого через `After=`/`Requires=`).
Foxbridge слушает только loopback — CDP наружу homelab не публикуется.

## Порядок работ

Все задачи — в этом каталоге. Сначала вертикальный минимум (PoC вручную), затем
совместимость, затем smoke test, затем декларативная упаковка и lifecycle.

1. [f18-01](./f18-01-poc-outside-nixos.md) — минимальный PoC вне NixOS: Camoufox через
   Foxbridge + Jev с `BU_CDP_URL`.
2. [f18-02](./f18-02-cdp-surface.md) — проверка CDP surface, который реально вызывает
   `jev_ultrafast/browser.py`; фиксируем только реальные несовместимости.
3. [f18-03](./f18-03-set-focus-emulation.md) — решение `Emulation.setFocusEmulationEnabled`
   на уровне Foxbridge (предпочтительно), Jev не патчим.
4. [f18-04](./f18-04-smoke-test.md) — smoke test полного пути: open → snapshot → click →
   type → wait DOM → `DONE`.
5. [f18-05](./f18-05-camoufox-fingerprint.md) — проверка, что Foxbridge не ломает fingerprint
   Camoufox (в т.ч. viewport из `Emulation.setDeviceMetricsOverride`).
6. [f18-06](./f18-06-humanize.md) — проверка `humanize=true` Camoufox; если humanization не
   применяется к обычным CDP input events — фиксируем как отдельное ограничение.
7. [f18-07](./f18-07-systemd-split.md) — разделение browser runtime и Jev на два systemd
   сервиса, связь через `BU_CDP_URL`.
8. [f18-08](./f18-08-nixos-module.md) — декларативный NixOS-модуль
   (`modules/services/foxbridge-camoufox.nix`, `modules/services/jev-ultrafast.nix`),
   секреты через agenix (не в Nix store).
9. [f18-09](./f18-09-lifecycle.md) — lifecycle: восстановление после падения Camoufox/
   Foxbridge, повторное подключение harness, отсутствие zombie, корректное `Agent.close()`.
10. [f18-10](./f18-10-pi-browser-task.md) — интерфейс для Pi: один tool `browser_task(url, goal)`,
    запускающий оригинальный `Agent(url, goal)`.
11. [f18-11](./f18-11-integration-test.md) — интеграционный тест полного пути на ноде +
    реальная задача Jev (Google Flights или пример из его examples).
12. [f18-12](./f18-12-acceptance.md) — приёмка по всем acceptance criteria.

## Осознанно откладываем

- Писать собственную Jev policy, заменять `snapshot.js`, переносить Jev на Playwright,
  использовать `camofox-browser` snapshots/refs, Playwright MCP внутри Jev, Selenium,
  browser pool, proxy rotation, собственный cursor humanization, менять алгоритм
  `observe → choose → act` — всё это **вне** задачи (явный «не делать»).
- Несколько параллельных браузеров, proxy profiles, сессии, интеграция browser lifecycle
  с Lattice/r1s — после стабилизации минимального вертикального среза.

**Критерий готовности:** оригинальный `jev-ultrafast` запускается с `BU_CDP_URL`
(без локальных изменений), браузером фактически является Camoufox, между ними работает
Foxbridge, Jev закрывает минимум одну полноценную web-задачу (click/type/navigation/
`Runtime.evaluate(awaitPromise=true)`), fingerprint Camoufox сохраняется, стек стартует
декларативно на NixOS через systemd, CDP недоступен извне хоста, есть smoke/integration
test полного пути.
