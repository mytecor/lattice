# Собрать полный жизненный цикл worker

Фича: [F10 — disposable worker](./README.md). Зависит от f10-01 и f10-02.

## Контекст

До controller жизненный цикл запускается вручную, но уже должен иметь те же конечные состояния и
cleanup semantics.

## Что сделать

- [ ] Реализовать `start → clone → environment → execute → publish → destroy` как явную state flow.
- [ ] Создавать уникальные workspace/run identifiers без переиспользования checkout.
- [ ] Гарантировать cleanup при success, failure, timeout и operator cancellation.
- [ ] Сохранять до уничтожения только объявленные result/artifact references и diagnostics.

## Критерий готовности

- [ ] Все четыре terminal paths уничтожают workspace и execution state.
- [ ] После завершения на worker host нет данных, обязательных для продолжения task.

## Затрагиваемые файлы / слои

- worker lifecycle service/scripts
- integration checks
- operations documentation

## Открытые вопросы

_нет_.
