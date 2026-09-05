# Выдавать worker минимальные временные credentials

Фича: [F10 — disposable worker](../features/f10-disposable-worker.md). Зависит от f9-02, f9-05 и
f10-02.

## Контекст

Worker нужны доступы к конкретному repository, LLM gateway и artifact upload, но не provider
credentials и не полномочия других tasks.

## Что сделать

- [ ] Описать scope и lifetime credentials для Git, gateway и artifacts.
- [ ] Доставлять credentials только после создания worker и отзывать при terminal state.
- [ ] Исключить credentials из task specification, logs, commits, artifacts и reusable images.
- [ ] Проверить cross-task, expired-token и post-destroy deny cases.

## Критерий готовности

- [ ] Worker выполняет task с минимальным набором доступов и не видит provider credentials.
- [ ] Credential не работает для чужого repository/task или после завершения lease/run.

## Затрагиваемые файлы / слои

- worker credential broker/injection
- `KEY_MANAGEMENT.md`
- security checks

## Открытые вопросы

_нет_.
