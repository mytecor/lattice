# Развернуть Verdaccio как npm proxy/cache

Фича: [F9 — cache и artifact plane](./README.md). Зависит от F8.

## Контекст

npm, pnpm и yarn должны получать пакеты через общий cache, который можно удалить и восстановить из
upstream registries и lockfiles.

## Что сделать

- [ ] Добавить закреплённый Verdaccio service и persistent cache directory под `/var/cache`.
- [ ] Настроить upstream registry, limits, auth boundary и безопасное логирование.
- [ ] Подключить npm/pnpm/yarn clients из tool profile без глобальной ручной настройки.
- [ ] Проверить cold/warm install по lockfile и поведение после очистки cache.

## Критерий готовности

- [ ] Три поддерживаемых package managers используют один proxy endpoint.
- [ ] Cold install после удаления cache даёт тот же dependency graph по lockfile.

## Затрагиваемые файлы / слои

- `modules/verdaccio/`
- `profiles/cache-plane/`
- `profiles/pi/`

## Открытые вопросы

_нет_.
