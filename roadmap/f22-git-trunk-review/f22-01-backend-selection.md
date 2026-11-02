# Выбрать Git review/trunk backend

Фича: [F22](./README.md). Целевой процесс — [AGENT_WORKFLOW.md](./../AGENT_WORKFLOW.md).

## Что сделать

- [ ] Провести PoC Gerrit на обычном Git без Radicle API: загрузка change, новый patch set,
      review, независимый Verified, отзыв/устаревание approval и submit.
- [ ] Проверить модель «одна задача → один commit»: локальная рабочая история, checkpoints,
      подготовка squash-кандидата, сохранение Change-Id. Не рассчитывать на автоматический
      squash произвольной многокоммитной ветки при Gerrit submit.
- [ ] Проверить разделение credentials автора/reviewer/checker/submitter, запрет прямого push
      и самоодобрения, ограничение права конкретным кандидатом. Отметить необходимые адаптеры.
- [ ] Спроектировать интеграцию файлов задач вне Git-ядра: автоматический приём допустимого
      состояния, системное создание review-задач, требования checks и подтверждение политики.
- [ ] Сопоставить с небольшим `merged` над bare Git: аутентифицированные операции approval,
      подписанные записи в служебных refs и восстановимые индексы вместо отдельной review DB.
- [ ] Сравнить эксплуатацию, стоимость собственной реализации, поддержку RNS/mesh окружения,
      миграцию review state и варианты distributed deployment совместно с F23.
- [ ] Зафиксировать ADR и последующие задачи выбранного backend, сохранив независимость от
      репликации и формата задач. До ADR ни Gerrit, ни собственный протокол не считать выбранным.

## Критерий готовности

- [ ] Воспроизводимый PoC, таблица поддерживаемых требований/недостающих механизмов и явный выбор.
- [ ] Определено, кто выдаёт полномочия reviewer и как принимающая сторона отличает их от
      произвольного Git author или поля задачи.

## Материалы для проверки

- [Gerrit: Changes](https://gerrit-review.googlesource.com/Documentation/concept-changes.html).
- [Gerrit: NoteDb](https://gerrit-review.googlesource.com/Documentation/note-db.html).
- [Gerrit: Submit requirements](https://gerrit-review.googlesource.com/Documentation/config-submit-requirements.html).
- [Gerrit: multi-primary](https://gerrit-review.googlesource.com/Documentation/glossary.html).
