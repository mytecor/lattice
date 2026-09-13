# Определить версионируемую task specification

Фича: [F10 — disposable worker](./README.md). Зависит от F8 и F9.

## Контекст

Task должен восстанавливаться без Pi session из repository state, task specification и явно
сохранённых результатов.

## Что сделать

- [ ] Определить versioned schema для repository/ref, objective, model class, environment, tools,
  inputs, limits и ожидаемых outputs.
- [ ] Отделить declarative task data от runtime lease/worker state и secrets.
- [ ] Определить validation, forward/backward compatibility и canonical serialization.
- [ ] Добавить минимальный и полный fixture tasks с отрицательными проверками.

## Критерий готовности

- [ ] Task specification однозначно валидируется и не содержит provider credentials.
- [ ] Два чистых runner запуска получают эквивалентные входы из одной specification и Git ref.

## Затрагиваемые файлы / слои

- task schema/contracts
- fixtures/checks
- `ARCHITECTURE.md`

## Открытые вопросы

_нет_.
