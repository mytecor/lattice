# f18-06. Проверить `humanize` Camoufox

## Контекст

Camoufox имеет режим `humanize=true`. Надо проверить, применяется ли humanization
к обычным CDP input events, которые шлёт Jev (`Input.dispatchMouseEvent`,
`Input.dispatchKeyEvent`, `Input.insertText`) через Foxbridge/Juggler. Семантику Jev
не меняем; собственный HumanCursor в рамках задачи не добавляем.

## Что сделать

- [ ] Проверить возможность запуска Camoufox с `humanize=true` через Foxbridge.
- [ ] Убедиться, что Jev по-прежнему шлёт обычные CDP input events (методы выше).
- [ ] Определить, применяет ли Camoufox humanization (естественные задержки /
      траектории) к таким событиям через Foxbridge/Juggler.
- [ ] Если нет — зафиксировать это как отдельное ограничение (в ответе к задаче и,
      при необходимости, в BACKLOG).

## Критерий готовности (Definition of Done)

- [ ] Есть документированный ответ: применяется ли humanization к CDP input от Jev.
- [ ] Jev не менялся, собственного HumanCursor нет; ограничение (если есть) зафиксировано.

## Затрагиваемые файлы / слои

- Foxbridge (запуск Camoufox с `humanize`).
- Jev **не трогаем**.

## Открытые вопросы

_нет_
