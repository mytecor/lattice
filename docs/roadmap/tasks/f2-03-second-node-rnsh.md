# Поднять второй узел; включить `profiles/rnsh` на обоих

Фича: [F2 — Reticulum поверх TCP/IP](../features/f2-reticulum-tcp.md).

## Контекст

Для того чтобы проверить межузловую связь, нужен второй узел, и на обоих узлах должен работать
`profiles/rnsh` (listener remote shell через Reticulum).

## Что сделать

- [ ] Создать второй узел по образцу `nodes/example`.
- [ ] Включить `profiles/rnsh` на обоих узлах.
- [ ] Подключить TCP-интерфейсы ([f2-01](./f2-01-reticulum-tcp-interfaces.md)) на обоих.
- [ ] Проверить, что оба узла видят друг друга через Reticulum.

## Критерий готовности

- [ ] Два узла обмениваются данными через Reticulum поверх TCP/IP.
- [ ] `rnsh`-listener активен на обоих.

## Затрагиваемые файлы / слои

- `nodes/` (второй узел, новый)
- `profiles/rnsh`
- `profiles/rns-server`

## Открытые вопросы

_нет_
