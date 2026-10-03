# Реализовать Git reconciler `taskd`

Фича: [F11 — Git task pipeline](./README.md). Зависит от f11-01 и f11-02.

## Контекст

`taskd` переводит Git state в actionable `TaskSpec`, вызывает узкую границу
`TaskSpec → TaskResult` и публикует результат обратно в Git. Correctness не зависит от cursor или
локальной базы.

## Что сделать

- [ ] Добавить `packages/taskd/` и `modules/taskd/`.
- [ ] На старте получить current head, просканировать task repository и reconcile все actionable
      revisions до начала watch loop.
- [ ] Продолжить через повторяющийся `git.watch` от head, повторяя вызов после `no_change`.
- [ ] Вести только теряемый mapping `task revision → currently running request`.
- [ ] Вызвать `agentd` через meshbus contract `TaskSpec → TaskResult`.
- [ ] Не передавать и не хранить r1s execution ID, ACP session, tunnel, agent iteration или lease.

## Критерий готовности

- [ ] Пустой локальный state после restart полностью восстанавливается startup scan Git.
- [ ] Изменение `task.md` создаёт новую revision, а terminal revision больше не запускается.

## Затрагиваемые файлы / слои

- `packages/taskd/`
- `modules/taskd/`
- meshbus TaskSpec/TaskResult contracts
- reconciliation tests

## Открытые вопросы

_нет_.
