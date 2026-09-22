# f18-03. Решить `Emulation.setFocusEmulationEnabled`

## Контекст

`jev_ultrafast/browser.py` вызывает `Emulation.setFocusEmulationEnabled` — Jev использует
его для предотвращения throttling background tab. Foxbridge, вероятно, пока не реализует
этот метод. Jev патчить нельзя — чиним на уровне Foxbridge.

## Что сделать

- [ ] Проверить, действительно ли вызов падает (запустить через f18-02 чек-лист).
- [ ] Если падает — добавить compatibility implementation в Foxbridge (приемлемо
      как честная реализация focus emulation поверх Juggler, так и безопасный no-op,
      возвращающий `{}` без ошибки).
- [ ] Подтвердить, что после fix Jev продолжает работать без изменений своего кода.
- [ ] Если полноценная реализация невозможна и no-op недостаточен — зафиксировать
      ограничение (что именно деградирует: throttling, focus events) отдельной записью,
      не трогая Jev.

## Критерий готовности (Definition of Done)

- [ ] `Emulation.setFocusEmulationEnabled` через Foxbridge не падает и отвечает успешным
      результатом.
- [ ] Upstream `jev-ultrafast` запускается без локальных изменений (цель задачи).

## Затрагиваемые файлы / слои

- Код Foxbridge (compatibility handler или no-op для `setFocusEmulationEnabled`).
- Jev — **не трогаем**.

## Открытые вопросы

_нет_
