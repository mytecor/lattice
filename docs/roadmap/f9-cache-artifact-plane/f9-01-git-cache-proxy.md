# Развернуть Git cache proxy

Фича: [F9 — cache и artifact plane](./README.md). Зависит от
[f4-01](../f4-payload/f4-01-radicle-seed-comin.md) и F8.

## Контекст

Кандидат `rolandjitsu/git-cache-proxy` должен ускорять повторные clone/fetch через локальные bare
mirrors. Cache хранится на локальной POSIX FS и не является source of truth.

## Что сделать

- [ ] Проверить кандидата на нужных Git transport/auth flows и закрепить версию.
- [ ] Добавить NixOS service с отдельным пользователем и cache directory под `/var/cache`.
- [ ] Направить тестовый clone/fetch через proxy и подтвердить cache hit.
- [ ] Ограничить сеть и filesystem права сервиса его назначением.

## Критерий готовности

- [ ] Повторный clone/fetch использует локальный bare mirror и даёт тот же commit graph.
- [ ] Удаление mirror приводит к обычному refetch, а не к потере source.

## Затрагиваемые файлы / слои

- `modules/git-cache-proxy/`
- `profiles/cache-plane/`
- `nodes/mytecor-homelab/`

## Открытые вопросы

Окончательный выбор proxy подтверждается проверкой private repository auth flow.
