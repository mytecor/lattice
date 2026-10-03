# Собрать OCI agent runtime с ACP endpoint

Фича: [F10 — agent runtime](./README.md). Зависит от
[f8-05](../f8-pi-runtime/f8-05-pi-rpc-contract.md),
[f10-01](./f10-01-package-r1s.md) и [f10-02](./f10-02-deploy-r1sd.md).

## Контекст

Прежний план одноразового Pi RPC runner заменён long-lived ACP endpoint внутри r1s workload.
`agentd` будет ACP client, а r1s tunnel — транспортом до контейнера. Отдельный worker RPC и
host-side запуск Pi через `PI_ACP_PI_COMMAND` не являются execution boundary F10.

## Что сделать

- [ ] Собрать immutable OCI image с Git, общим Pi package/tool profile, `pi-acp` и ACP listener.
- [ ] Реализовать entrypoint, который из явных source repository/revision создаёт workspace и
      запускает ACP endpoint на выделенном `ACP_PORT`.
- [ ] Передавать task context и capabilities отдельно от image; не встраивать credentials и
      уникальное task state в reusable layers.
- [ ] Обеспечить доступ к LLM gateway и разрешённым tools без host networking и без доступа к
      `containerd.sock`.
- [ ] Поддержать r1s tunnel до `ACP_PORT` и smoke ACP session с реальным Pi.
- [ ] Добавить deterministic bootstrap/RPC smoke с fake gateway и real Git workspace.

## Критерий готовности

- [ ] r1s запускает image, ACP client открывает session через tunnel и Pi выполняет smoke task.
- [ ] Новый контейнер из тех же явных входов стартует без session/workspace предыдущего.
- [ ] В image и ACP protocol нет Lattice-specific queue, scheduler или task lifecycle.

## Затрагиваемые файлы / слои

- agent runtime package/image
- NixOS integration для запуска workload
- [`modules/pi`](../../modules/pi/README.md)
- [`profiles/pi`](../../profiles/pi/README.md)
- integration checks

## Открытые вопросы

_нет_.
