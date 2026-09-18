# F13. Web-клиент ACP (acp-components)

Браузерный клиент поверх закреплённого ACP ingress из [F8](../f8-pi-runtime/README.md): готовый
open-source workbench [`zvzuola/acp-components`](https://github.com/zvzuola/acp-components)
(React-компоненты + framework-agnostic core) вместо самописного UI. Цель — дать второму
независимому ACP-клиенту (браузеру в LAN) те же возможности, что подтверждены для Ferngeist:
мульити-сессии, tool calls, permissions, стриминг и подключение к живым сессиям через один
endpoint `ws://acp.<nodename>.local/`.

Зависит от [F8](../f8-pi-runtime/README.md) — транспорт и daemon уже закреплены
([f8-06](../f8-pi-runtime/f8-06-network-acp-daemon.md)), новая вертикаль только потребляет
существующий ingress/session plane и ничего не меняет в disposable-модели F10.

Задачи: [f13-01](./f13-01-deploy-acp-components.md).

**Критерий готовности:** клиент развёрнут декларативно (закреплённый источник, воспроизводимая
сборка/раздача), подключается к существующему LAN endpoint `ws://acp.<nodename>.local/` без
изменения декларативной конфигурации daemon, создаёт параллельные сессии и возобновляет их после
reconnect; результат acceptance зафиксирован в задаче (включая отрицательный результат, если
совместимость с `acp.v1`-формой соединения не подтвердится).

**Осознанно откладываем:** публикация клиента за пределами доверенной LAN (граница trusted LAN из
[f8-06](../f8-pi-runtime/f8-06-network-acp-daemon.md) не меняется); auth-вопросы остаются в
записи 4 [BACKLOG.md](../BACKLOG.md); desktop-обёртки (Tauri) и кастомизация UI сверх проверки
совместимости.
