# f18-01. Минимальный PoC вне NixOS-модуля

## Контекст

Первая задача F18. Нужно проверить связку вручную на одной машине, до какой-либо
NixOS-упаковки, чтобы понять реальные несовместимости и подтвердить жизнеспособность
схемы `Jev → browser-harness → Foxbridge → Camoufox`.

## Что сделать

- [x] Установить вне NixOS: `jev-ultrafast` (оригинальный `browser-use/jev-ultrafast`),
      `browser-harness` (приходит как dependency Jev), Foxbridge, Camoufox.
- [x] Запустить Camoufox через Foxbridge с Juggler backend.
- [x] Убедиться, что Foxbridge открывает локальный CDP endpoint
      (например `http://127.0.0.1:9222`), и проверить его руками (curl `/json/version`,
      `/json`).
- [x] Запустить Jev с `BU_CDP_URL=http://127.0.0.1:9222`.
- [x] Зафиксировать минимальный рабочий скрипт/документацию ручного запуска (команды,
      версии пакетов, какой порт слушает Foxbridge, лог Camoufox).

## Фактический прогон (2026-09-22, mytecor-homelab.local)

Проведено в `/root/f18-poc/` на ноде с временных venv/бинарников, **без** NixOS-модулей.

Версии:

- `jev-ultrafast` 0.1.0 (оригинальный `browser-use/jev-ultrafast`), venv через `uv`;
  python 3.12.14 из nixpkgs.
- `browser-harness` 0.1.13 — dependency Jev (daemon `python -m browser_harness.daemon`,
  резолвит WS через `{BU_CDP_URL}/json/version → webSocketDebuggerUrl`).
- Foxbridge v0.1.1 — собран `go install github.com/VulpineOS/foxbridge/cmd/foxbridge@latest`
  (на NixOS потребовался `gcc` для cgo).
- Camoufox 152.0.4-beta.30 — каталог
  `/root/.cache/camoufox/browsers/official/152.0.4-beta.30-5720d45b/`; бинарь прогнан
  через `patchelf --set-interpreter` на загрузчик NixOS glibc.

Запуск Foxbridge (loopback-only CDP на `127.0.0.1:9222`):

```sh
cd /root/f18-poc
setsid bash run-foxbridge-f18.sh </dev/null >logs/foxbridge-f18.log 2>&1 &
curl -s http://127.0.0.1:9222/json/version
# {"Browser":"foxbridge/1.0","Protocol-Version":"1.3",
#  "webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/foxbridge"}
```

`run-foxbridge-f18.sh` собирает `LD_LIBRARY_PATH` из всех `/nix/store/*/lib` и запускает
`foxbridge-f18 --binary <camoufox> --port 9222 --headless`.

Smoke-скрипт `jev-cdp-test.py` (без изменений Jev, только `BU_CDP_URL`):

```python
import os
os.environ["BU_CDP_URL"] = "http://127.0.0.1:9222"
from jev_ultrafast.browser import Browser
b = Browser("data:text/html,<html><body><h1 id=h>hello f18</h1><input id=i></body></html>")
print(b.evaluate('document.getElementById("h").textContent'))   # 'hello f18'
print(b.evaluate("navigator.userAgent"))                        # Camoufox UA
print(b.evaluate("navigator.webdriver"))                        # False
b.close()
```

**Результат:** полный проход прошёл — `evaluate` вернул `'hello f18'`, UA — настоящий
Camoufox, `navigator.webdriver == False`, `b.close()` чистый.

Нюансы воспроизведения:

- standalone-бинарь `uv` на NixOS не работает — брался `uv` из nixpkgs.
- На NixOS потребовалась сборка wheel-ников (`greenlet`/`lxml`/`orjson`) из исходников
  и shim `camoufox-python` с `LD_LIBRARY_PATH` на gcc-библиотеки.
- Собранный/патченный Foxbridge (см. f18-02/f18-03) живёт на ноде:
  `/root/f18-poc/foxbridge-src3/` + бинари `foxbridge-f18v2…v6` (актуальная — v6,
  текущая в `run-foxbridge-f18.sh`).

## Критерий готовности (Definition of Done)

- [x] Camoufox реально поднят через Foxbridge и отдаёт CDP на loopback.
- [x] Jev с одним только `BU_CDP_URL` стартует и доходит до браузера (не про `Jev сам
      не должен знать ничего про Camoufox или Foxbridge`).
- [x] Шаги воспроизводимы по зафиксированной инструкции (не по памяти).

## Затрагиваемые файлы / слои

- Вне NixOS: временный каталог ручного PoC (вне репозитория или `docs/`-заметка).
- `roadmap/f18-browser-agent-stack/` — результаты в задачах f18-02/f18-04.
- Метка: никакие `modules/` не трогаем на этом шаге.

## Открытые вопросы

- Версии Foxbridge/Camoufox/`jev-ultrafast` на момент запуска; способ установки
  (npm/pnpm global, uv, собранная сборка) — решается по факту на машине.
