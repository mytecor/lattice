# Radicle source bootstrap; `comin` на radicle-remote

Фича: [F4 — полезная нагрузка](../features/f4-payload.md).

## Контекст

Цель F4: конфигурация сети распространяется без обязательного GitHub. На текущем этапе Radicle
работает на основной NixOS-ноде; второй постоянной NixOS-ноды в плане нет. Нужно сделать Radicle
рабочим source origin для `comin` и будущих disposable workers, сохранив GitHub как независимый
remote. Отдельно решается первичная загрузка: чистая нода должна получить конфигурацию до того,
как сможет обновляться из локального Radicle storage.

## Что сделать

- [ ] Декларативно поднять Radicle service на `mytecor-homelab` и закрепить нужные repositories.
- [ ] Проверить clone/fetch нужного commit через Radicle с другого чистого клиента.
- [ ] Переключить `comin` на radicle-remote как основной источник, GitHub — зеркало.
- [ ] Решить проблему первичной загрузки radicle-хранилища на новый узел.

## Критерий готовности

- [ ] `mytecor-homelab` применяет конфиг из Radicle при недоступном GitHub.
- [ ] Новый узел получает radicle-хранилище и сам обновляется из него.

## Затрагиваемые файлы / слои

- `profiles/radicle`, `profiles/gitops`
- `nodes/`

## Открытые вопросы

Bootstrap Radicle — открытое решение №1 в [BACKLOG.md](../BACKLOG.md).
