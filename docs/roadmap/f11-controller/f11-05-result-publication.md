# Зафиксировать публикацию commit, result и artifacts

Фича: [F11 — controller](./README.md). Зависит от f11-01, f11-04 и f9-05.

## Контекст

Завершение task связывает Git commit/push, машинно-читаемый result и immutable artifacts, не
перенося большие outputs в controller storage.

## Что сделать

- [ ] Определить result envelope со status, source/base/result refs, attempt и artifact manifests.
- [ ] Проверять, что заявленный commit достижим из разрешённого repository и соответствует attempt.
- [ ] Зафиксировать порядок publication/finalization и reconciliation неизвестного исхода.
- [ ] Ограничить размер inline logs/results; большие outputs публиковать как artifacts.

## Критерий готовности

- [ ] По terminal task state однозначно находятся проверяемые commit и artifact refs.
- [ ] Повторная публикация того же attempt идемпотентна, другого — обнаруживается как конфликт.

## Затрагиваемые файлы / слои

- result schema/contracts
- controller finalization
- integration tests

## Открытые вопросы

_нет_.
