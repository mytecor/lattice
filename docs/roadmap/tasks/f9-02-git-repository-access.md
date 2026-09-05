# Ограничить Git proxy по репозиториям

Фича: [F9 — cache и artifact plane](../features/f9-cache-artifact-plane.md). Зависит от f9-01.

## Контекст

Proxy-side upstream credentials не должны превращать доступ к одному репозиторию в доступ ко всем
private Git objects. Workers не получают upstream GitHub credentials и не читают cache directory.

## Что сделать

- [ ] Определить client identity и repo-scoped authorization policy.
- [ ] Разделить proxy-side upstream credentials по минимально необходимым repositories.
- [ ] Закрыть прямой filesystem и network доступ workers к bare mirrors и upstream credentials.
- [ ] Проверить allow/deny cases и отсутствие данных запрещённого repo в ответах/cache metadata.

## Критерий готовности

- [ ] Разрешённый client клонирует только явно выданные repositories.
- [ ] Запрещённый repository и cache directory недоступны, даже если объект уже закеширован.

## Затрагиваемые файлы / слои

- `modules/git-cache-proxy/`
- `profiles/cache-plane/`
- `KEY_MANAGEMENT.md`
- security checks

## Открытые вопросы

Механизм client identity выбирается после проверки возможностей proxy в f9-01.
