# Развернуть r1sd-allocator на ноде

Фича: [F10 — agent runtime](./README.md). Зависит от
[f10-01](./f10-01-package-r1s.md) (r1s/r1sd доступны как flake-пакеты — закрыта 2026-09-16)
и от nut-up звена исполнения на ноде. Прокладывает путь к
[f10-04](./f10-04-agent-runtime-acp.md) (контейнерный ACP agent runtime через r1s).

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

- [x] Создать NixOS-модуль `lattice.worker-runtime` в `modules/worker-runtime/`
      (`options.nix` / `config.nix` / `default.nix` + `README.md` по конвенциям проекта).
- [x] Включить `containerd` как зависимость службы, дать `r1sd` доступ к его сокету
      (`containerd.sock`), не открывая наружу.
- [x] Запускать `r1sd` как foreground systemd-сервис (`ExecStart = lib.getExe' pkgs.lattice.r1sd`),
      `wantedBy = [ "multi-user.target" ]`, `after` network/containerd/общий RNS shared instance.
- [x] Строгий системный песочник по образцу `pi-acp-daemon` (`AF_UNIX` // `AF_INET`/`AF_INET6`
      для RNS, `NoNewPrivileges`, `ProtectSystem=full`, без capabilities), контролируемый опциями.
- [x] Предусмотреть опции под каталог состояний; состояние — в `StateDirectory`/`RuntimeDirectory`,
      persist ноды для переживания reboot.
- [x] Включить модуль на ноде `mytecor-homelab` (в `nodes/mytecor-homelab/config.nix`,
      условно по наличию секрета оператора) и прогнать `nix flake check` (eval-часть локально;
      полная сборка — в CI/GHA, см. ниже).
- [x] **Smoke-проверка подъёма** (на живой ноде, 2026-10-03): `worker-runtime` активен,
      а `r1s`-клиент с той же ноды доходит до allocator (инициализирует транспорт, видит
      кластер), без live workload.

## Критерий готовности (Definition of Done)

- [x] `r1sd`-сервис активен на ноде как декларативно объявленный systemd-юнит и переживает
      `reboot`/перезагрузку сервиса без ручных действий.
- [x] `r1s`-клиент с той же ноды успешно соединяется с allocator (smoke-проверка пройдена),
      что подтверждает развёртывание backend-звена перед `f10-04`/`f10-06`.

> Блокируется живой нодой и оператором: `r1sd` не стартует без cluster-join-токена, который
> оператор создаёт как agenix-секрет `r1s-cluster-token.age` (см. раздел «Что осталось»).
> Модуль уже детектирует наличие секрета (`pathExists`) и включает сервис только при нём.

## Реализация (2026-09-24)

Модуль `lattice.worker-runtime`: `r1sd` поднимается как foreground systemd-сервис от выделенного
пользователя `r1s` (uid/gid 634) в строгом песочнике. Включает `virtualisation.containerd` и
открывает его gRPC-сокет `/run/containerd/containerd.sock` только группе `r1s` (0660, gid = `r1s`;
наружу сокет не публикуется). `r1sd` запускается с `--rns-config` (рендер Reticulum-Go из опции
`rnsUplinks`), `--identity` (авто-генерация ключа в `StateDirectory`), `--cluster` (join через
`preStart` один раз), capacity/node/containerd-флаги.

**Канал доступа — RNS, не локальный сокет.** По интерфейсу r1s подтверждено: `r1sd` общается по
Reticulum (RNS), а не через loopback/AF_UNIX. На момент выполнения f10-02 отдельно исследовался
локальный сокет `r1s serve`; позднейший архитектурный cutover убрал Lattice-owned worker broker и
`worker.sock`. Актуальный `agentd` использует r1s как library и ACP tunnel по модели из
[TASK_EXECUTION.md](../../TASK_EXECUTION.md).

**Общий реестр peers, без дублирования.** Модуль не хранит список Reticulum peers: `rnsUplinks`
по умолчанию `{ }`, а нода задаёт их из общего
[`profiles/networking/reticulum.nix`](../../profiles/networking/reticulum.nix) (тот же реестр, что
использует `rns-network` для rns-server). Отдельный `--rns-config` обязателен: `r1sd` использует
**Reticulum-Go** — независимый RNS-стек, который не подключается к уже работающему процессу
`rns-server` (rns-rs) как к shared instance.

### F22 cutover (2026-09-26): переписывание под shared-instance контракт

С пин v0.4.0 (F22, `739f26ee…`) модуль переписан — исторический блок выше описывает пре-F22
реализацию и оставлен как запись о том, что было. Что изменилось:

- **`--rns-config` удалён.**: `r1s`/`r1sd` больше не строят собственный Reticulum-стек. Оба бинарника
  подключаются как **клиенты** к уже работающему общему RNS shared instance по platform-default
  сокету (`@rns/default` или TCP 37428) и замыкаются (`ErrSharedInstanceUnavailable`), если его
  нет — никогда не становятся сервером. Поэтому `rnsUplinks`/`rnsLogLevel`/`rnsConfigFile` и
  рендер Reticulum-конфига из модуля удалены.
- **Кластер — позиционно, членство per-user.** Кластер передаётся `r1sd <флаги> <cluster-id>`
  (уникальный hex-префикс публичного ID). Членство хранится как per-user credential в
  `$HOME/.config/r1s/clusters/<id>` (`r1sd cluster init|join|list`); join-токен `r1s1:<...>`
  потребляется один раз в `preStart` из agenix-секрета.
- **`HOME` = `StateDirectory`.** Кластерные credentials разрешаются через `os.UserHomeDir`;
  сервис экспортирует `HOME=/var/lib/worker-runtime`, иначе системный юзер (deфолт `HOME=/var/empty`)
  не смог бы прочитать/сохранить их, а состояние не пережило бы reboot.
- **Shared-instance юнит** — через `rnsInstanceService` (дефолт `rns-server`); toggle `rnsShared`
  удалён, т.к. shared-instance поведение F22 безусловно.

## Что осталось (оператор + живая нода)

1. **Сделано (2026-09-27).** Join-токен кластера r1s сгенерирован (через закреплённый
   `internal/cluster` r1s: `cluster.Generate`, 32 байта случайности) и зашифрован как
   `nodes/mytecor-homelab/secrets/r1s-cluster-token.age` для реципиентов `[admin node]`
   (соглашение `secrets.nix`). Публичный `Cluster ID`: `fea879387416a033216590028a2ee8776790ced4e2af949fbdcc7e1215d3a5b3`.
   Проверено: расшифровка и identity ноды (`/persist/var/lib/lattice/age/identity`), и admin
   (Mac) — `NODE-DECRYPT-OK`, содержимое `r1s1:<...>` 49 байт. Токен в stdout/контекст не
   выводился (генерируется через stdin в `age --encrypt`, печатается только публичный ID).
   readable пользователем `r1s` (owner/group `r1s`, mode `0400`).
2. **Сделано (развёрнуто на живой ноде 2026-09-30 по comin).** containerd и `worker-runtime`
   активны: `worker-runtime.service` — `active (running)`, пережил перезагрузку ноды (без
   ручных действий); `containerd` — `active`.
3. **Сделано (smoke на живой ноде 2026-10-03).** В журнале allocator готов:
   `r1sd ready identity=ef33e0... destination=63410d17...` — allocator имеет идентичность
   и дестинацию в RNS shared instance. Кластер зарегистрирован: `r1s cluster list` от
   пользователя `r1s` (HOME=`/var/lib/worker-runtime`) показывает `fea879...`.
   Клиент `r1s run <cluster-id>` от пользователя `r1s` инициализирует транспорт
   (`Created ephemeral transport identity`, `Path request handler initialized`) и доходит до
   стадии запроса workload — allocator достижим из клиента.
4. **Осталось (не блокирует).** Полная `nix flake check` (включая `go test ./...` r1s
   и содержимое-проверки контракт-теста) — в GitHub Actions на x86_64-linux.

## Затрагиваемые файлы / слои

- `modules/worker-runtime/` (новый модуль: `options.nix`, `config.nix`, `default.nix`, `README.md`)
- `nodes/mytecor-homelab/config.nix` (включение модуля; секрет `r1s-cluster-token.age`; persist
  `/var/lib/worker-runtime`)
- `packages/r1s/` (без изменений — только потребление)
- `tests/worker-runtime.nix` (новый контракт-тест)
- roadmap status: `roadmap/f10-disposable-worker/README.md`

## Открытые вопросы

_нет_ — закрыты реализацией:

- **Канал доступа**: только RNS (Reticulum); `r1sd` не слушает loopback/AF_UNIX. Клиентский
  локальный сокет `r1s serve` — к f10-04.
- **Restart-политика**: `on-failure` / `RestartSec=5` (дефолт Lattice), как у других сервисов.
- **Привилегии в песочнике**: `r1sd` не требует root — он только gRPC-клиент containerd
  (сам подъём OCI делает containerd-daemon). Поэтому строгий песочник без capabilities безопасен;
  сокет открыт только группе `r1s`. `--tunnel-enabled` выключен (для f10-02 live workload не нужен).
- **Права**: отдельный системный пользователь `r1s` (не root).
