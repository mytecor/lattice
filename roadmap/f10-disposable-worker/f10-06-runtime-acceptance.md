# Провести acceptance disposable ACP runtime

Фича: [F10 — agent runtime](./README.md). Зависит от
[f10-04](./f10-04-agent-runtime-acp.md) и
[f10-05](./f10-05-agent-credentials.md).

## Контекст

Главный инвариант F10 проверяется уничтожением runtime. ACP session и workspace не обязаны
переживать execution; новый контейнер обязан восстановить исходную готовность из явных входов.

## Что сделать

- [x] Запустить agent container через r1s, открыть ACP session по tunnel и выполнить Git change.
- [x] Принудительно уничтожить execution вместе с ACP session и workspace.
- [x] Запустить новый container из тех же source/task inputs и создать новую ACP session.
- [x] Проверить отсутствие уникального task state, credentials и build directories после cleanup.
- [x] Зафиксировать наблюдаемый lifecycle и границы ответственности r1s/runtime.

## Критерий готовности

- [x] Повторный запуск не требует данных прежней ACP session или filesystem контейнера.
- [x] Agent image и bootstrap воспроизводимы, tunnel доступен, credentials ограничены.
- [x] F10 не реализует recovery task, verifier или result publication: это границы F11/F19.

## Затрагиваемые файлы / слои

- end-to-end checks
- agent runtime runbook
- roadmap status

## Результаты acceptance (2026-10-16)

1. **Запуск и выполнение изменений**:
   - На хосте `mytecor-homelab.local` запущен workload через `r1s run -p 15514:55514 '{"workload":{"image":"127.0.0.1:5050/ipfs/<CID>@sha256:<digest>"},"policy":{},"resourceClass":"default"}'`.
   - Проверено создание Git-репозитория в `/workspace` и запуск `hydra-acp-daemon` на loopback порту 55514.
   - Через туннель выполнен WebSocket handshake ACP (`initialize`, `session/list`), а в рабочем каталоге контейнера создан и закоммичен файл `hello.txt`.
2. **Принудительное уничтожение**:
   - Контейнер и клиентский процесс `r1s` принудительно остановлены и удалены через containerd (`nerdctl --namespace r1s rm -f`).
3. **Повторный запуск и проверка изоляции**:
   - Повторный запуск из идентичной спецификации workload создал новый изолированный контейнер со свежим `/workspace` (только `.gitkeep` и первоначальный коммит, без следов `hello.txt` или `.pi-acp/`).
   - Новая ACP-сессия открыта успешно через тот же порт туннеля.

## Открытые вопросы

_нет_.
