# Определить границы вычислений и хранилища

Фича: [F4 — полезная нагрузка](./README.md).

## Контекст

Цель Lattice: «реплика кода, вычисления и хранилище, шлюзы наружу». Архитектурный разбор
зафиксировал, что вычисления выполняются disposable workers, а физическая worker-нода не хранит
уникальное состояние. Общая persistent filesystem не входит в базовую архитектуру.

## Что сделать

- [x] Разделить source of truth, artifacts, control-plane state и disposable caches.
- [x] Зафиксировать отсутствие общей persistent FS как зависимости workers.
- [x] Первоначально разложить вычислительный контур на Pi runtime, disposable workers и controller
      в F7–F11; позднейший cutover на Git/taskd/agentd зафиксирован в
      [TASK_EXECUTION.md](../../TASK_EXECUTION.md).

## Критерий готовности

- [x] Для каждого вида состояния назначен source of truth.
- [x] Дальнейшая реализация вычислений и хранения разложена на отдельные фичи и задачи.

## Затрагиваемые файлы / слои

- [ROADMAP.md](../../ROADMAP.md)
- [TASK_EXECUTION.md](../../TASK_EXECUTION.md)

## Открытые вопросы

_нет_. Object storage и worker isolation выбираются в соответствующих задачах. Прежний открытый
вопрос controller storage снят: durable task/result state хранится в Git.

**Статус:** выполнена 2026-09-05 как архитектурное планирование; актуальная реализация разложена в
F10, F11 и F19–F21 по [TASK_EXECUTION.md](../../TASK_EXECUTION.md).
