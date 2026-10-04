# Собрать immutable OCI agent runtime с IPFS distribution

Фича: [F10 — agent runtime](./README.md). Зависит от
[f10-01](./f10-01-package-r1s.md) (упаковка r1s/r1sd — закрыта 2026-09-16),
[f10-02](./f10-02-deploy-r1sd.md) (разворачивание r1sd-allocator — закрыта 2026-10-03) и
[f8-05](../f8-pi-runtime/f8-05-pi-rpc-contract.md) (Pi RPC contract).

Прежний план одноразового Pi RPC runner заменён long-lived ACP endpoint внутри r1s workload.
`agentd` будет ACP client, а r1s tunnel — транспортом до контейнера. Отдельный worker RPC и
host-side запуск Pi через `PI_ACP_PI_COMMAND` не являются execution boundary F10.

**Distribution:** OCI image публикуется и распространяется через IPFS. Ни GHCR, ни Docker Hub, ни
центральный OCI registry в схеме не участвуют. Nix отвечает за воспроизводимую сборку образа;
IPFS — за распространение; containerd/r1s — за execution.

## Контракт образа

OCI image — immutable и reusable. Из одних и тех же Nix inputs получается один и тот же digest.

### Image содержит

```
git                      — workspace operations
Pi                       — coding agent
pi-acp                   — ACP stdio adapter
pi-tool-profile          — lattice-pi-tool-profile env (base tools)
agent runtime / bootstrap — workspace init из source repo+revision
ACP listener             — long-lived endpoint на выделенном ACP_PORT
CA certificates          — для TLS-доступа к gateway
minimal runtime deps     — shell, curl, jq, ssh, gnupg …
```

### Image НЕ содержит

```
task state / session
source repository (репо приходит как явный вход при запуске)
source revision
credentials
ACP session state
unique task data
```

Новый контейнер всегда восстанавливает исходную готовность из явных входов.

## Что сделать

### 1. Nix agent image derivation

- [x] Создать пакет `packages/agent-image/` с `pkgs.dockerTools.buildLayeredImage` (или
      `pkgs.nix-docker-tools.buildImage`, если есть), который собирает OCI image из Nix closure
      agent runtime. Image экспортируется как `packages.${system}.agent-image` в `flake.nix`.
- [x] Включить в image все компоненты из «содержит»:
      `git`, `lattice-pi-tool-profile`, `pkgs.lattice.pi-acp` (с `pkgs.lattice.pi` в PATH),
      bootstrap script, ACP listener entrypoint, CA certs.
- [x] Зафиксировать reproducibility: deterministic layer ordering, no timestamps, no non-determinism
      sources (randomness seed, `/dev/urandom` заморозить или не использовать).
- [x] Entrypoint (`ENTRYPOINT` / `CMD`) запускает bootstrap: читает `SOURCE_REPO` / `SOURCE_REVISION`
      / `ACP_PORT` / `ACP_SECRET` из environment, создаёт workspace, стартует ACP listener.
      Bootstrap не принимает shell-команды, только объявленные env vars.
- [x] Доступ к LLM gateway и разрешённым tools — без host networking и без доступа к
      `containerd.sock` внутри контейнера (см. также ограничения ниже).
- [x] Добавить `packages/agent-image/default.nix` и `flake.nix` exports.
- [x] Smoke: `nix build .#packages.x86_64-linux.agent-image` — получить итоговый OCI image archive
      и проверить, что `nerdctl load` в containerd на живой ноде загружает image с ожидаемым
      набором файлов.

### 2. Публикация образа: nerdctl + IPFS

- [x] Добавить на живой ноде (и в module) `nerdctl` — через `virtualisation.containerd.enable`
      `containerd` уже есть, `nerdctl` — через `pkgs.nerdctl` или NixOS-опцию, доступную в PATH.
- [x] Написать publish script / derivation `scripts/publish-agent-image.sh`:

      ```sh
      nix build .#packages.x86_64-linux.agent-image
            ↓
      nerdctl load --input result/  (→ local containerd)
            ↓
      nerdctl push ipfs://<gateway> -- ipfs-daemon-addr <ipfs-api-socket> result
            ↓
      IPFS CID
            ↓
      Resolve CID → OCI manifest digest
            ↓
      ImagePublication { cid: bafy…, digest: sha256:… }
      ```

- [x] `nerdctl push ipfs://…` пушит в локальный (или указанный) Kubo daemon. Убедиться, что
      daemon слушает API (или использует `--ipfs-stack gateway` в nerdctl).
- [x] **Не считать OCI digest до push окончательным**: `nerdctl` может преобразовать representation
      при публикации. После push resolve'ить CID обратно и получить итоговый OCI manifest digest,
      который реально будет проверять containerd/r1s.
- [x] Publish script выводит `ImagePublication { cid, digest }` в stdout как JSON или в файл
      `result/publication.json`. Это единственный артефакт, который передаётся дальше в execution.
- [x] Не публиковать в GHCR, Docker Hub или любой центральный registry.

### 3. IPFS runtime на allocator node

- [x] Создать NixOS-модуль `modules/ipfs-registry-facade/` (или расширить `modules/worker-runtime/`):
      - `Kubo` (`pkgs.kubo` / `pkgs.go-ipfs`) — IPFS daemon, слушает локально, API на `/ip4/127.0.0.1/tcp/5001`.
      - `nerdctl ipfs registry serve` (containerd в режиме OCI facade over IPFS) — слушает на
        `127.0.0.1:5050`, преобразует OCI Registry API → IPFS.
- [x] Настроить Kubo: pinning опубликованного образа по CID (persistent, survives reboot).
      `IPFS_PATH` / `KUBO_PATH` → `/var/lib/ipfs-daemon`.
- [x] Registry facade не публикуется наружу: слушает только на loopback. В будущем, если нужен
      multi-node replication — через IPFS Bitswap между нодами, не через HTTP registry.
- [x] Включить модуль на `mytecor-homelab` (conditionally, рядом с `worker-runtime`).
- [ ] Smoke: запустить `ipfs daemon` + `nerdctl ipfs registry serve` на ноде; с live Kubo от
      allocator ноды вытащить образ по CID и загрузить в containerd без участия центрального
      registry.

### 4. Интеграция с r1s (без изменений)

- [x] `r1s` не меняется. Контракт сохраняется: standard OCI reference + mandatory digest pinning +
      `containerd.Pull()`.
- [x] Execution reference для IPFS-опубликованного образа имеет вид:

      ```
      127.0.0.1:5050/ipfs/<CID>@sha256:<digest>
      ```

      Именно этот reference получает workload при запуске на allocator.
- [x] Сохранить существующую проверку в r1s:

      ```
      requested OCI digest == pulled OCI target digest
      ```

- [x] IPFS CID не заменяет OCI digest и не ослабляет pinning. Две identity хранятся рядом:
      - `CID` (IPFS content address) — определяет, что лежит в IPFS.
      - `sha256:…` (OCI manifest digest) — определяет, что проверяет containerd и r1s.
- [x] Убедиться, что `nerdctl ipfs registry serve` корректно возвращает OCI manifest с тем
      digest, который был resolved после publish.

### 5. Nix flake exports

- [x] Добавить `packages.${system}.agent-image` в `flake.nix` → экспорт Nix-пакета OCI образа.
- [x] Добавить `apps.${system}.publish-agent-image` → publish script как flake-app (или Nix run
      target), чтобы на ноде было достаточно:

      ```sh
      nix run .#publish-agent-image
      ```

      а результат — `result/publication.json` с CID и digest.
- [x] Добавить `apps.${system}.pull-agent-image` → resolve CID → pull в containerd (для cold path).
- [x] Проверить: `nix build .#agent-image` воспроизводимо создаёт image; два последовательных
      билда дают идентичный digest.

### 6. Smoke test: end-to-end

- [ ] **Hot path:**

      ```
      nix build .#agent-image
              ↓
      nerdctl load (→ containerd)
              ↓
      nerdctl push ipfs://... (→ Kubo)
              ↓
      получить CID + OCI digest
              ↓
      nerdctl pull 127.0.0.1:5050/ipfs/<CID>@sha256:<digest> (→ containerd через registry facade)
              ↓
      r1s run --image 127.0.0.1:5050/ipfs/<CID>@sha256:<digest> <cluster-id>
              ↓
      ACP endpoint внутри контейнера доступен через r1s tunnel
              ↓
      Pi выполняет smoke task
      ```

- [ ] **Cold path:**

      ```
      удалить локальную копию image из containerd (ctr images rm)
              ↓
      повторить r1s run с тем же reference
              ↓
      containerd получает image через IPFS registry facade
              ↓
      ACP endpoint доступен → cold pull прошёл
      ```

- [ ] Проверить, что image без credentials делает bootstrap из пустого workspace, а не падает
      или не подменяет данные другого task.

## Критерий готовности

- [ ] `nix build .#agent-image` воспроизводимо создаёт OCI image с git, Pi, pi-acp, tool profile и
      ACP listener; два последовательных билда дают идентичный digest.
- [ ] Image публикуется через `nerdctl push ipfs://...` в локальный Kubo, минуя GHCR/Docker Hub.
- [ ] Публикация возвращает `ImagePublication { cid: bafy…, digest: sha256:… }` и итоговый digest
      resolve'ится после push (не до).
- [ ] На allocator работает локальный IPFS-backed OCI registry (`127.0.0.1:5050`) — Kubo + nerdctl
      registry facade.
- [ ] `r1s` без изменений запускает image по digest-pinned OCI reference.
- [ ] Cold pull реально проходит через IPFS: после удаления из containerd cache containerd получает
      образ через registry facade.
- [ ] ACP endpoint внутри контейнера доступен через r1s tunnel; Pi выполняет smoke task.
- [ ] Для полного цикла не требуются GHCR, Docker Hub или центральный registry.

## Затрагиваемые файлы / слои

- `packages/agent-image/` — новый пакет (derivation Nix → OCI image)
- `scripts/publish-agent-image.sh` — publish script
- `scripts/pull-agent-image.sh` — cold-pull script (опционально, может быть частью module)
- `modules/ipfs-registry-facade/` — новый модуль (Kubo + nerdctl registry serve)
- `flake.nix` — exports `agent-image`, publish/pull apps
- `nodes/mytecor-homelab/config.nix` — включение модуля
- `tests/` — smoke-тесты (hot path + cold path)

## Что _не_ делать

```
GHCR / Docker Hub / central private registry      — не добавлять
OCI distribution внутри r1s protocol              — не добавлять
IPFS logic внутри r1s                             — не добавлять
containerd.sock внутрь agent container            — не добавлять
task-specific state в image                       — не добавлять
replication factor / pin reconciliation / GC       — отложить после cold-pull proof
private IPFS swarm / cluster-specific auth         — отложить после cold-pull proof
```

## Зависимости между подзадачами

```
1 (Nix image)     → 2 (publish)     → 3 (IPFS runtime) → 4 (r1s integration)
                  ↓                 ↓
                  5 (flake exports) → 6 (smoke tests)
```

Tasks 1 и 3 независимы и могут идти параллельно. Tasks 2, 4, 5 зависят от 1 и 3. Task 6
зависит от всех.

## Открытые вопросы

- **Как resolve'ить OCI digest после IPFS push?** Варианты:
  - `nerdctl push ipfs://...` возвращает CID; затем `nerdctl inspect --netptr <ref>` после pull
    обратно даёт digest. Но это двухшаговая операция. Альтернатива — посмотреть в OCI manifest
    внутри containerd store после pull и вытащить `config.digest`.
  - Возможно, `nerdctl push ipfs://...` уже возвращает digest в stdout/stderr — проверить.
  - IPFS-only: написать wrapper, который берёт CID → `ipfs dag get <cid>/manifest` → parse OCI
    manifest → извлечь digest. Но это хрупко.
  - **Решение:** после push сделать `nerdctl tag <local-name> localhost:5050/ipfs/<CID>` →
    `nerdctl push localhost:5050/ipfs/<CID>` → digest из remote. Или pull с `--platform linux/amd64`
    обратно и inspect.

  Точный механизм resolve зафиксировать после первого live test на ноде.

- **Kubo daemon vs go-ipfs vs Kubo module в NixOS:** проверить, есть ли готовый NixOS-модуль
  для Kubo (`services.kubo` или эквивалент в nixpkgs), чтобы не писать unit вручную.
  **Решение:** в `nixpkgs` есть официальный модуль `services.kubo`, который настраивает
  `ipfs.service`, репозиторий в `dataDir` и API-сокет. Модуль `ipfs-registry-facade`
  использует `services.kubo` для управления Kubo daemon.

- **Version pinning образа:** при update Nix-инпута агентского образа (новый Pi, новый pi-acp)
  image digest меняется. Должен быть способ сказать allocator «используй этот конкретный CID +
  digest» — это будет часть credential/injection в f10-05.

## Реализация (2026-10-04)

Реализованы компоненты 1–5 и контрактные тесты:

1. **OCI agent image derivation** ([`packages/agent-image`](../../packages/agent-image/README.md)):
   собирается через `pkgs.dockerTools.buildLayeredImage` с фиксированной эпохой
   (`created = "1970-01-01T00:00:01Z"`), содержит `git`, `pi-tool-profile`, `pi`, `pi-acp`,
   `hydra-acp`, CA certs, fakeNss и entrypoint-скрипт `bootstrap.sh`. При старте скрипт
   читает `SOURCE_REPO`, `SOURCE_REVISION`, `ACP_PORT`, `ACP_SECRET`, `WORKSPACE_DIR`,
   настраивает git workspace и запускает `hydra-acp-daemon` на `127.0.0.1:${ACP_PORT}`.
2. **Скрипты публикации и загрузки**:
   - [`scripts/publish-agent-image.sh`](../../scripts/publish-agent-image.sh) — выполняет
     `nerdctl load`, пушит в IPFS через `nerdctl push ipfs://...`, разрешает OCI manifest
     digest через локальный containerd и формирует `result/publication.json`.
   - [`scripts/pull-agent-image.sh`](../../scripts/pull-agent-image.sh) — холодный pull через
     локальный фасад `127.0.0.1:5050/ipfs/<CID>@sha256:<digest>`.
   - Оба скрипта экспортируются как `packages` и `apps` (`publish-agent-image`, `pull-agent-image`).
3. **NixOS-модуль IPFS Registry Facade** ([`modules/ipfs-registry-facade`](../../modules/ipfs-registry-facade/README.md)):
   декларативно поднимает `services.kubo` с хранилищем в `/var/lib/ipfs-daemon` и systemd-юнит
   `ipfs-registry-facade` (`nerdctl ipfs registry serve --listen-registry 127.0.0.1:5050`).
   Включён на ноде `mytecor-homelab` синхронно с `worker-runtime` при наличии секрета кластера,
   данные IPFS зафиксированы в impermanence-списке `/persist`.
4. **Контрактные проверки**:
   [`tests/ipfs-registry-facade.nix`](../../tests/ipfs-registry-facade.nix) и
   [`tests/agent-image.nix`](../../tests/agent-image.nix) проверяют корректность конфигураций,
   параметров entrypoint/bootstrap, флагов registry facade и публикации в `nix flake check`.
5. **Осталось на живой ноде (Task 6)**:
   - Проверены hot/cold path публикации образа в IPFS и pull через фасад `127.0.0.1:5050`.
   - В `bootstrap.sh` адрес слушателя `hydra-acp-daemon` зафиксирован на loopback `127.0.0.1` (устранена ошибка `Refusing to bind to non-loopback host 0.0.0.0 without TLS configured`).
   - Финальный end-to-end smoke `r1s run -p 15514:55514` заблокирован недетерминированным
     discovery allocator'а в `r1s`: первый `requestAttempt` не имеет известных destination'ов и
     ждёт только новые события `endpoint.Discoveries()` в пределах `offerWait`, тогда как `r1sd`
     по умолчанию повторяет announce раз в 5 минут. Клиент, запущенный между announce-пакетами,
     завершается с `no usable offer` раньше следующего объявления.
   - Локальная доставка через `rns-rs` подтверждена: после нового announce от перезапущенного
     allocator'а клиент сразу получает discovery. Поэтому текущие наблюдения не доказывают дефект
     `rns-rs`; предупреждения `invalid announce signature` и ошибки egress требуют отдельной
     диагностики и не считаются установленной причиной этого таймаута.
   - Предположение о пропущенном `a.start()` в detached child опровергнуто чтением полного пути
     запуска: родитель повторно запускает бинарник без `-d`, с `--r1s-child`, после чего общий
     `run()` запускает transport до входа в `runDetachedChild`. Дополнительный `a.start()` там не
     требуется.
   - Исправление должно дать новому run-клиенту известный allocator destination или иной
     детерминированный bootstrap/rendezvous до истечения `offerWait`. Обычный Reticulum
     `PathRequest` принимает уже известный destination hash и не является поиском сервиса по
     aspect, поэтому задача не сводится к вызову `RequestPath` с неизвестным allocator'ом.
     Уменьшение `announceInterval` с достаточным запасом относительно `offerWait` допустимо как
     временная проверка, но не заменяет устойчивый механизм discovery.
   - Миграция общего daemon с patched `rns-rs` на Reticulum-Go ведётся отдельно в
     [f3-05](../f3-reticulum-tcp/f3-05-reticulum-go-daemon.md). Она убирает межреализационную
     shared-instance boundary, но не заменяет bootstrap allocator destination в `r1s`.
