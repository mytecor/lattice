# Развернуть r1sd-allocator на ноде

Фича: [F10 — disposable worker](./README.md). Зависит от
[f10-01](./f10-01-package-r1s.md) (r1s/r1sd доступны как flake-пакеты — закрыта 2026-09-16)
и от nut-up звена исполнения на ноде. Прокладывает путь к
[f10-04](./f10-04-pi-rpc-runner.md) (контейнерный Pi runtime через r1s-запросы).

## Контекст

[f10-01](./f10-01-package-r1s.md) закрепил r1s как flake-пакет: `r1s` (клиент) и `r1sd`
(allocator) собираются `buildGoModule` и экспортируются в `packages.${system}`. Но собрать
бинарники ещё не значит развернуть backend: `r1sd` — это allocator, выполняющий OCI workload
через `containerd`, и на ноде его нужно поднять декларативно, как остальные сервисы Lattice, —
через NixOS-модуль с systemd-юнитом, а не вручную. Пустой каталог `modules/worker-runtime/`
в репозитории уже зарезервирован под этот модуль.

Задача замыкает оставшийся разрыв между «пакет собран» и «backend доступен worker'у»: после
неё на ноде работает выделенный `r1sd`-сервис, `r1s`-клиент с той же ноды до него добирается,
а сервис переживает перезагрузку (impermanence/persist учтены).

## Что сделать

- [ ] Создать NixOS-модуль `lattice.worker-runtime` в `modules/worker-runtime/`
      (`options.nix` / `config.nix` / `default.nix` + `README.md` по конвенциям проекта).
- [ ] Включить `containerd` как зависимость службы, дать `r1sd` доступ к его сокету
      (`containerd.sock`), не открывая наружу.
- [ ] Запускать `r1sd` как foreground systemd-сервис (`ExecStart = lib.getExe' pkgs.lattice.r1sd`),
      `wantedBy = [ "multi-user.target" ]`, `after` network/containerd.
- [ ] Строгий системный песочник по образцу `pi-acp-daemon` (loopback/`AF_UNIX` + локальный
      сокет, `NoNewPrivileges`, `ProtectSystem`, без лишних capabilities), контролируемый опциями.
- [ ] Предусмотреть опции под host/listen socket allocator и каталог состояний; состояние —
      в `StateDirectory`/`RuntimeDirectory`, при необходимости в `environment.persistence` ноды.
- [ ] Включить модуль на ноде `mytecor-homelab` (в `nodes/mytecor-homelab/config.nix`)
      и прогнать `nix flake check`.
- [ ] Smoke-проверка подъёма: `r1sd` активен, а `r1s`-клиент с той же ноды доходит до
      allocator (health/простой вызов), без live workload.

## Критерий готовности (Definition of Done)

- [ ] `r1sd`-сервис активен на ноде как декларативно объявленный systemd-юнит и переживает
      `reboot`/перезагрузку сервиса без ручных действий.
- [ ] `r1s`-клиент с той же ноды успешно соединяется с allocator (smoke-проверка пройдена),
      что подтверждает развёртывание backend-звена перед `f10-04`/`f10-06`.

## Затрагиваемые файлы / слои

- `modules/worker-runtime/` (новый модуль: `options.nix`, `config.nix`, `default.nix`, `README.md`)
- `nodes/mytecor-homelab/config.nix` (включение модуля; persist для состояния)
- `packages/r1s/` (без изменений — только потребление)
- roadmap status: `roadmap/f10-disposable-worker/README.md`
- инвентаризация портов/реестра, если allocator слушает TCP

## Открытые вопросы

- Какой канал доступа использует `r1sd`: только `AF_UNIX` сокет (например
  `/run/lattice/worker.sock`) или ещё loopback TCP? Уточнить по интерфейсу r1s и зафиксировать
  в модуле/README.
- Нужен ли `retry`/`Restart`-policy, отличный от `on-failure` дефолта других сервисов Lattice.
- Требует ли `r1sd` привилегий, которых нет в строгом песочнике (запуск OCI через containerd),
  — и как это согласовать с «не открывать наружу».
- С какими правами бежит `r1sd` (отдельный `r1s`-user или root как у `pi-acp-daemon`).
