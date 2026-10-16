# Выдавать agent container минимальные credentials

Фича: [F10 — agent runtime](./README.md). Зависит от
[f10-04](./f10-04-agent-runtime-acp.md).

## Контекст

Agent runtime нужны доступы к конкретному source repository, LLM gateway и разрешённым внешним
capabilities, но не provider credentials, чужие repositories или control plane r1s.

## Что сделать

- [x] Описать scope и lifetime credentials для Git, gateway и каждой capability.
- [x] Передавать credentials workload'у после назначения r1s execution, не сохраняя их в image,
      task specification, logs, commits или artifacts.
- [x] Не давать контейнеру доступ к allocator credentials, `containerd.sock` или секретам других
      task revisions.
- [x] Обеспечить отзыв/истечение credentials независимо от terminal result.
- [x] Проверить cross-task, expired-token и post-destroy deny cases.

## Критерий готовности

- [x] Контейнер выполняет smoke task с минимальным набором доступов и не видит provider/control
      plane credentials.
- [x] Credential не работает для чужого repository/task или после завершения lifetime.

## Затрагиваемые файлы / слои

- agent runtime credential injection
- [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md)
- security checks

## Результаты реализации (2026-10-16)

- Контейнер изолирован в отдельном mount/network namespace без доступа к `containerd.sock`, хостовой файловой системе или секретам `agenix`.
- Allocator credentials (`/var/lib/worker-runtime/identity`) защищены `StateDirectoryMode = "0700"` и не монтируются внутрь workload.
- Сессионный ACP-токен генерируется на лету (`ACP_SECRET`) и валидируется демоном `hydra-acp` по WebSocket-субпротоколу `hydra-acp-token.<token>`.
- Спецификация workload передаёт только scoped environment переменные. После завершения execution и остановки контейнера все ephemeral данные уничтожаются.

## Открытые вопросы

_нет_.
