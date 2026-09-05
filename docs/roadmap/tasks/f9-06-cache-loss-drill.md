# Доказать disposable-семантику caches

Фича: [F9 — cache и artifact plane](../features/f9-cache-artifact-plane.md). Зависит от f9-01,
f9-03, f9-04 и f9-05.

## Контекст

Название `cache` недостаточно: нужно доказать, что Git/Verdaccio/Attic data не содержит
единственный экземпляр состояния, необходимого для корректности.

## Что сделать

- [ ] Зафиксировать inventory cache directories и их authoritative upstreams.
- [ ] Выполнить одинаковую sample build с тёплыми caches и после их безопасной очистки.
- [ ] Сравнить source revision, dependency locks, build result digest и artifact manifest.
- [ ] Добавить runbook восстановления и наблюдаемость hit/miss без требований к correctness.

## Критерий готовности

- [ ] После потери всех caches sample workflow завершается тем же логическим результатом.
- [ ] Единственный ожидаемый эффект — дополнительное время/трафик; artifacts остаются доступны.

## Затрагиваемые файлы / слои

- `profiles/cache-plane/`
- acceptance scripts/checks
- operations runbook

## Открытые вопросы

_нет_.
