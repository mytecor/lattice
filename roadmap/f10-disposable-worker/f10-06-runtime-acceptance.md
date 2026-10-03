# Провести acceptance disposable ACP runtime

Фича: [F10 — agent runtime](./README.md). Зависит от
[f10-04](./f10-04-agent-runtime-acp.md) и
[f10-05](./f10-05-agent-credentials.md).

## Контекст

Главный инвариант F10 проверяется уничтожением runtime. ACP session и workspace не обязаны
переживать execution; новый контейнер обязан восстановить исходную готовность из явных входов.

## Что сделать

- [ ] Запустить agent container через r1s, открыть ACP session по tunnel и выполнить Git change.
- [ ] Принудительно уничтожить execution вместе с ACP session и workspace.
- [ ] Запустить новый container из тех же source/task inputs и создать новую ACP session.
- [ ] Проверить отсутствие уникального task state, credentials и build directories после cleanup.
- [ ] Зафиксировать наблюдаемый lifecycle и границы ответственности r1s/runtime.

## Критерий готовности

- [ ] Повторный запуск не требует данных прежней ACP session или filesystem контейнера.
- [ ] Agent image и bootstrap воспроизводимы, tunnel доступен, credentials ограничены.
- [ ] F10 не реализует recovery task, verifier или result publication: это границы F11/F19.

## Затрагиваемые файлы / слои

- end-to-end checks
- agent runtime runbook
- roadmap status

## Открытые вопросы

_нет_.
