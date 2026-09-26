# Worker runtime module (r1sd allocator)

Модуль `lattice.worker-runtime` (F10 — disposable worker) разворачивает
[`r1sd`](../../packages/r1s/package.nix) — allocator исполняемого backend Lattice — как
декларативный foreground systemd-сервис, аналогично остальным сервисам ноды. Задача
[f10-02](../../roadmap/f10-disposable-worker/f10-02-deploy-r1sd.md).

> Пин r1s обновлён до v0.4.0 (F22, 2026-09-26). F22 — ломающий cutover: у `r1s`/`r1sd` больше
> **нет** `--rns-config` и частного Reticulum-стека, кластер передаётся **позиционно** по ID, а
> членство кластера хранится как per-user credential в `~/.config/r1s/clusters/<id>`. Модуль
> переписан под этот контракт (см. «RNS shared instance» ниже).

## Что делает

- Включает `virtualisation.containerd` и открывает его gRPC-сокет
  (`/run/containerd/containerd.sock`) только для группы `r1s` (0660, group = r1s). Сокет
  остаётся на loopback-юникс, наружу не публикуется.
- Запускает `r1sd` от выделенного системного пользователя `r1s` в строгом песочнике:
  только `AF_UNIX` (containerd-сокет и shared-instance RNS-сокет) и исходящий
  `AF_INET`/`AF_INET6` (tunnel data plane при `--tunnel-enabled`), без capabilities,
  `NoNewPrivileges`, `ProtectSystem=full`, `PrivateTmp`.
- Первый запуск (`preStart`) выполняет `r1sd cluster join <token>` из agenix-секрета и
  сохраняет **ID кластера** в `StateDirectory` (`cluster-id`); daemon далее всегда стартует с
  позиционным селектором `r1sd <allocator-флаги> <cluster-id>`. Credential кластера живёт в
  `$HOME/.config/r1s/clusters/<id>` (под `StateDirectory`) и переживает перезагрузку.

## Канал доступа к allocator

`r1sd` общается по **RNS** (Reticulum), а не через локальный loopback/AF_UNIX сокет. Поэтому
«/run/lattice/worker.sock» из диаграммы F10 — это не канал allocator'а, а клиентский локальный
сокет `r1s serve` (используется на шаге f10-04, когда worker-брокер обращается к r1s).
Данный модуль поднимает сам allocator: `r1s`-клиент с той же ноды доходит до него по RNS.

## RNS shared instance (F22)

Начиная с F22 `r1s`/`r1sd` **не строят собственный Reticulum-стек**: они подключаются как
клиенты к уже работающему **общему RNS shared instance** (`share_instance = Yes`) и замыкаются,
если его нет. Этим shared instance на ноде служит `lattice.rns-server` (профиль `rns-network`,
реализация rns-rs с `share_instance = true`), дефолтно отдающий сокет `@rns/default`. Модуль
больше не рендерит Reticulum-конфиг и не хранит список peers: интерфейсы/routing/who connects
к публичной сети RNS владеет rns-server, а worker лишь подключается к тому же shared instance,
что и остальные RNS-сервисы ноды.

Поэтому:

- Опции `rnsUplinks` / `rnsLogLevel` / `rnsConfigFile` / `rnsShared` удалены из контракта модуля:
  shared-instance поведение F22 безусловно; `r1sd` всегда клиент и замыкается при отсутствии
  shared instance (`ErrSharedInstanceUnavailable`), поэтому включить/выключить его нельзя.
- Сервис `worker-runtime` ордерится `after`/`wants` за shared-instance-юнитом и containerd
  (`rnsInstanceService`, дефолт `rns-server`).
- Сервис экспортирует `HOME=<StateDirectory>`: членство кластера живёт в
  `$HOME/.config/r1s/clusters/<id>` (распознаётся через `os.UserHomeDir`), и системные юзеры
  systemd иначе получили бы `HOME=/var/empty` (read-only, non-persistent).

Совместимость rns-rs↔Reticulum-Go на wire-уровне — предмет живого smoke-теста (см. критерий
f10-02): rns-rs сервер протестирован против Python RNS, а r1s-клиент — против Go/Python shared
instance. Если smoke покажет расхождение протокола, альтернатива — развернуть выделенный
`reticulum-go` daemon (`share_instance = Yes`) и указать его юнит через `rnsInstanceService`.

## Требуется provisioning (оператор)

`lattice.worker-runtime` требует один агент-секрет — **join-токен кластера** r1s (`r1s1:...`).
Он подаётся как путь к файлу (agens-секрет, 0600, читаемый пользователем `r1s`). Токен
вычитывается только в `preStart` (runtime) и не попадает ни в Nix store, ни в argv `ExecStart`,
ни в журнал (команда `cluster join` печатает только Cluster ID и путь credential, не токен).

Identity allocator'а — **не секрет**: `r1sd` сам генерирует файл ключа при первом старте в
`/var/lib/<stateDirectory>/identity`, и он переживает перезагрузку через
`StateDirectory`/impermanence. (Аналогично тому, как `rad-peer auth` генерирует peer-ключ ноды
на месте.) Опция `identityFile` позволяет подать запечённый identity, если он нужен.

Включение на ноде (по конвенции «модуль включает сервис только когда .age-секрет создан
оператором»):

```nix
# nodes/<node>/config.nix
lattice.worker-runtime = lib.mkIf (builtins.pathExists ./secrets/r1s-cluster-token.age) {
  enable = true;
  clusterTokenFile = config.age.secrets.r1s-cluster-token.path;
  # необязательно: capacity, node-капабилитии, tunnel-enabled
};

age.secrets.r1s-cluster-token = lib.mkIf (builtins.pathExists ./secrets/r1s-cluster-token.age) {
  file = ./secrets/r1s-cluster-token.age;
  mode = "0400";
  owner = "r1s";
  group = "r1s";
};
```

Токен создаёт оператор (см. `r1s cluster init` на первом участнике, README r1s); расшифрованный agenix-секрет обязан монтироваться для пользователя `r1s`.

## Опции

| опция | тип | дефолт | смысл |
| --- | --- | --- | --- |
| `enable` | bool | `false` | включить модуль |
| `package` | package | `pkgs.lattice.r1s` | пакет с `r1s` и `r1sd` |
| `user` / `group` | str | `r1s` | системный пользователь/группа сервиса |
| `stateDirectory` | str | `worker-runtime` | имя `StateDirectory` под `/var/lib` |
| `runtimeDirectory` | str | `worker-runtime` | имя `RuntimeDirectory` под `/run` |
| `capacity` | str | `default=1` | ресурсные capacity allocator'а (`--capacity`) |
| `node` | nullOr str | `null` | JSON node capabilities для placement (`--node`) |
| `announceInterval` | str | `5m` | интервал анонсов (`--announce-interval`) |
| `containerdAddress` | str | `/run/containerd/containerd.sock` | сокет containerd |
| `containerdNamespace` | str | `r1s` | namespace containerd |
| `containerdSnapshotter` | str | `""` | snapshotter (дефолт демона при пустом) |
| `rnsInstanceService` | str | `rns-server` | systemd-юнит shared RNS instance, после которого ордерится worker |
| `identityFile` | nullOr str | `null` | путь к identity (по умолчанию генерируется в StateDirectory) |
| `clusterTokenFile` | nullOr path | `null` | путь к файлу join-токена (требуется при `enable`) |

## Проверка

Контракт-тест [`tests/worker-runtime.nix`](../../tests/worker-runtime.nix) проверяет: сервис
запускается только при наличии `clusterTokenFile`, `ExecStart` несёт `--identity` и позиционный
селектор кластера (**без** `--rns-config`), `HOME` указывает на `StateDirectory` (для разрешения
cluster credentials из `~/.config/r1s/clusters`), сервис ордерится после rns-server и containerd,
сокет containerd настраивается под группу `r1s`, песочник не открывает лишних address families
и не даёт capabilities.

## Ограничения / что остаётся на f10-04

Данный модуль поднимает backend (allocator) и доказывает smoke-соединение клиента до него
([f10-02](../../roadmap/f10-disposable-worker/f10-02-deploy-r1sd.md)). Контракт worker →
`r1s serve` (локальный сокет `/run/lattice/worker.sock`), broker `external-job`
(`pi-lattice-workers`) и запуск контейнерного Pi через `r1s run` — отдельная работа f10-04.
Execution tunnels (`--tunnel-enabled`) выключены: для f10-02 live workload не запускается.
