# f18-03. Решить `Emulation.setFocusEmulationEnabled`

## Контекст

`jev_ultrafast/browser.py` вызывает `Emulation.setFocusEmulationEnabled` — Jev использует
его для предотвращения throttling background tab. Foxbridge, вероятно, пока не реализует
этот метод. Jev патчить нельзя — чиним на уровне Foxbridge.

## Фактическое решение (применено в Foxbridge v3, проверено 2026-09-22)

Вызов действительно падал на Foxbridge v0.1.1:

```text
RuntimeError: {'code': -32601, 'message': 'method not found: Emulation.setFocusEmulationEnabled'}
```

Решение — **no-op** в `pkg/bridge/emulation.go` (патч в применённой сборке, f18-02).
Полная совместимость через Jev-вызов подтверждена запуском `jev-cdp-test.py` → exit 0.

Фрагмент добавленного case:

```go
case "Emulation.setFocusEmulationEnabled":
	// No-op: Firefox (Juggler) has no focus-emulation concept and headless
	// Camoufox renders regardless of tab focus. Jev calls this to keep its
	// owned background tab rendering; returning success is the correct
	// contract. See roadmap/f18-browser-agent-stack/f18-03.
	return json.RawMessage(`{}`), nil
```

## Что сделать

- [x] Проверить, действительно ли вызов падает (запустить через f18-02 чек-лист).
- [x] Если падает — добавить compatibility implementation в Foxbridge (приемлемо
      как честная реализация focus emulation поверх Juggler, так и безопасный no-op,
      возвращающий `{}` без ошибки).
- [x] Подтвердить, что после fix Jev продолжает работать без изменений своего кода.
- [ ] Если полноценная реализация невозможна и no-op недостаточен — зафиксировать
      ограничение (что именно деградирует: throttling, focus events) отдельной записью,
      не трогая Jev. *(Пока не деградирует: headless Camoufox рендерит в фоне, см. f18-05.)*

## Критерий готовности (Definition of Done)

- [x] `Emulation.setFocusEmulationEnabled` через Foxbridge не падает и отвечает успешным
      результатом.
- [x] Upstream `jev-ultrafast` запускается без локальных изменений (цель задачи).

## Затрагиваемые файлы / слои

- Код Foxbridge (compatibility handler или no-op для `setFocusEmulationEnabled`).
- Jev — **не трогаем**.

## Открытые вопросы

_нет_
