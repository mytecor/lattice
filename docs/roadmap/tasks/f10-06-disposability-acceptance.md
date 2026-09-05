# Провести acceptance-тест уничтожения и восстановления worker

Фича: [F10 — disposable worker](../features/f10-disposable-worker.md). Зависит от f10-03, f10-04 и
f10-05.

## Контекст

Главный инвариант F10 проверяется уничтожением, а не аудитом конфигурации.

## Что сделать

- [ ] Выполнить задачу до промежуточного сохранённого результата и принудительно уничтожить worker.
- [ ] Создать новый worker из той же task specification, актуального Git state и artifact refs.
- [ ] Завершить задачу, опубликовать commit/result и снова уничтожить worker.
- [ ] Проверить отсутствие checkout, dependencies, build directories, Pi session и credentials.

## Критерий готовности

- [ ] Новый worker продолжает task только по объявленным source-of-truth данным.
- [ ] Финальный result проверяем, а на физической worker-ноде нет уникального task state.

## Затрагиваемые файлы / слои

- end-to-end checks
- worker runbook
- roadmap status

## Открытые вопросы

_нет_.
