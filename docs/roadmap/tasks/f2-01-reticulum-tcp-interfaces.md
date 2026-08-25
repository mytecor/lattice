# Добавить TCP-интерфейсы Reticulum в `rns-server`/`profiles/rns-server`

Фича: [F2 — Reticulum поверх TCP/IP](../FEATURES.md).

## Контекст

Сейчас `profiles/rns-server` поднимает только `AutoInterface` со `discovery_scope = "link"` — то
есть работает исключительно внутри одного broadcast-домена. Межузловой связи через интернет
пока нет. Нужно описать TCP-интерфейсы Reticulum (клиентские и серверные), чтобы узлы связывались
через интернет, а не только в LAN.

## Что сделать

- [ ] Добавить в `modules/rns-server` типизированные опции для TCP-интерфейсов (клиент и сервер).
- [ ] Заполнить их дефолтами в `profiles/rns-server`.
- [ ] Проверить, что конфиг Reticulum (typed-генерация ConfigObj) корректно включает TCP-интерфейсы
      при сборке.

## Критерий готовности

- [ ] Узел с `profiles/rns-server` поднимает TCP-интерфейс, а не только `AutoInterface`.
- [ ] Конфиг генерируется через typed-генерацию без ручных правок.

## Затрагиваемые файлы / слои

- `modules/rns-server`
- `profiles/rns-server`
- `nodes/example`

## Открытые вопросы

Сколько узлов с публичным адресом — см. [f2-02](./f2-02-define-entry-points.md) и
[BACKLOG.md](../BACKLOG.md).
