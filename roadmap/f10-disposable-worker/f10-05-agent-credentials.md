# Выдавать agent container минимальные credentials

Фича: [F10 — agent runtime](./README.md). Зависит от
[f10-04](./f10-04-agent-runtime-acp.md).

## Контекст

Agent runtime нужны доступы к конкретному source repository, LLM gateway и разрешённым внешним
capabilities, но не provider credentials, чужие repositories или control plane r1s.

## Что сделать

- [ ] Описать scope и lifetime credentials для Git, gateway и каждой capability.
- [ ] Передавать credentials workload'у после назначения r1s execution, не сохраняя их в image,
      task specification, logs, commits или artifacts.
- [ ] Не давать контейнеру доступ к allocator credentials, `containerd.sock` или секретам других
      task revisions.
- [ ] Обеспечить отзыв/истечение credentials независимо от terminal result.
- [ ] Проверить cross-task, expired-token и post-destroy deny cases.

## Критерий готовности

- [ ] Контейнер выполняет smoke task с минимальным набором доступов и не видит provider/control
      plane credentials.
- [ ] Credential не работает для чужого repository/task или после завершения lifetime.

## Затрагиваемые файлы / слои

- agent runtime credential injection
- [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md)
- security checks

## Открытые вопросы

_нет_.
