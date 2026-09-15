# Выбрать и реализовать границу изоляции worker

Фича: [F10 — disposable worker](./README.md). Зависит от
[f10-01](./f10-01-task-specification.md).

## Контекст

Pi не является sandbox. Unattended task получает отдельную OCI/containerd boundary с явными CPU,
memory, disk, network и filesystem правами. Nix декларативно задаёт immutable worker image,
worker classes/policy и host runtime, но не отдельные эфемерные container instances.

## Что сделать

- [ ] Зафиксировать threat model и ограничения выбранной OCI/containerd boundary; явно описать,
      какие требования потребуют будущего перехода на VM/microVM.
- [ ] Вынести сборку Pi runtime (`settings.json`, `models.json`, tool profile, packages/extensions)
      в переиспользуемое Nix value и собрать из тех же derivations общий immutable OCI image для
      оркестратора и workers.
- [ ] Описать в Nix worker classes: image digest, CPU/RAM/PID limits, network policy, writable
      paths, capabilities и право создавать вложенные workers. Не использовать
      `virtualisation.oci-containers` или `containers.<name>` для runtime instances.
- [ ] Импортировать image в `containerd` идемпотентно по digest при активации конфигурации; runtime
      создаёт и уничтожает instances динамически.
- [ ] Ограничить ресурсы, сеть, mounts, devices и host control sockets.
- [ ] Выдать контейнеру только узкий `/run/lattice/worker.sock`; не монтировать
      `containerd.sock`, host secrets или cache directories вне разрешённых endpoints.

## Критерий готовности

- [ ] Оркестратор и disposable worker создаются из одного закреплённого Pi image/config и
      соблюдают разные объявленные state/resource policies.
- [ ] Escape/secret-access negative checks не дают доступ к host control и provider credentials.
- [ ] Изменение worker class требует декларативного rebuild, а создание/уничтожение instance — нет.

## Затрагиваемые файлы / слои

- worker image/module/profile
- security checks
- [ARCHITECTURE.md](../../ARCHITECTURE.md)

## Открытые вопросы

_нет_. Для первого backend выбран OCI/`containerd` (через r1s `r1sd`); более сильная VM/microVM
boundary остаётся допустимой заменой после измеренного требования.

## Источник решения

Обсуждение «Замена r1s в lattice» зафиксировало локальный `containerd` в качестве временной
implementation до готовности r1s. r1s готов ([mytecor/r1s](https://github.com/mytecor/r1s));
декларативные image/classes и динамические runtime instances остаются прежними, но изоляция теперь
обеспечивается `r1sd`-allocator'ом, который использует тот же `containerd`.
