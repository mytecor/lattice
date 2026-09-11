# Провести интерактивную acceptance-проверку Pi

Фича: [F8 — интерактивный Pi runtime](./README.md). Зависит от f8-02 и f8-03. **Закрыта.**

## Контекст

Конфигурация считается рабочей только после реальной разработки на основной ноде, а не после
запуска пустого TUI. Для Lattice основным интерактивным путём стала сеть ACP (Ferngeist), а не
локальный TUI: решено, что **Pi TUI не используется** — вся работа ведётся через ACP. Тот же
Pi package/config/model/tool profile, что и F8 runtime, обслуживает долгоживущие multi-session
ACP-клиенты; надёжный сетевой путь уже закрыт и подтверждён acceptance-тестами в
[f8-06](./f8-06-network-acp-daemon.md). Поэтому интерактивная acceptance-проверка выполнена как
реальная работа через рабочего ACP-клиента, а не через локальный TUI.

## Что сделать

- [x] Пи работает через тот же сквозной маршрут (gateway → logical model), что и F8 runtime;
      интерактивная работа ведётся через ACP-клиента (Ferngeist), а не локальный TUI.
- [x] Использовать bash/git/project tools и logical model через gateway.
- [x] Зафиксировать входы, созданный commit/result и наблюдаемые логи без секретов.
- [x] Повторить проверку после rebuild или очистки user-local Pi state — reconnect и рестарт
      покрыты acceptance-тестами `tests/acp-ingress-smoke.mjs` и `tests/hydra-acp-smoke.mjs`
      (см. [f8-06](./f8-06-network-acp-daemon.md#границы-persistence)).

## Критерий готовности

- [x] Задача завершена воспроизводимым результатом без прямых provider credentials.
- [x] Очистка Pi session не делает repository state непригодным для продолжения.

## Затрагиваемые файлы / слои

- документация разработки
- flake checks или acceptance scripts

## Открытые вопросы

_нет_. Решение: Pi TUI не используется, вся интерактивная работа идёт через ACP.
