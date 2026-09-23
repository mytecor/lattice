# f15-02. Доступы публикации с ноды: Radicle peer-identity и GitHub deploy key

Фича: [F15 — Разработка с ноды](./README.md). Зависит от
[f15-01](./f15-01-workspace-checkout.md) — рабочий checkout и remote `publish` на ноде.

## Контекст

[DEPLOYMENT.md](../../DEPLOYMENT.md) требует публиковать `main` в Radicle и GitHub одним push;
Radicle — авторитетный источник для `lattice-comin-source-sync`, поэтому push только в GitHub
нормализатор может откатить. Сегодня публикация возможна только с Mac. Чтобы агент в ACP-сессии
публиковал сам, ноде нужны: (1) собственная Radicle peer-identity с правом push в RID Lattice,
(2) write-доступ к GitHub.

Модель идентичности — независимые ключи на устройство: radicle-ключ ноды **генерится на ноде**,
не копируется с Mac. Radicle-слой ноды сейчас — seed-профиль (`rad-system`), его ключ не для
peer-пуша, поэтому peer-identity живёт в отдельном `RADICLE_HOME`. Секреты по
[AGENTS.md](../../AGENTS.md): расшифровки не попадают в контекст агента — только пути/файлы.

## Что сделать

- [x] 1. **Radicle peer-identity ноды.** Пакет `pkgs.lattice.rad-peer` (flake overlay): `rad`
      против отдельного peer-профиля `RAD_HOME=/persist/var/lib/radicle-peer` — не мешает
      seed-профилю `rad-system` (`/var/lib/radicle`). Собственно генерацию ключа делает
      оператор на ноде разово вручную: `rad-peer auth` (ключ генерится на месте).
- [x] 2. **Делегирование в RID Lattice.** `rad id update` добавить ноду как делегата/подписанта
      (подпись — ключом оператора с Mac). Выбрать и зафиксировать порог подписей
      (1-of-2 или 2-of-2, см. «Открытые вопросы»). **Сделано 2026-09-23**: порог
      1-of-2; revision `d888fa4` (delegates = [mytecor, peer-DID ноды], threshold 1) принят
      оператором с Mac (`rad id update --delegate … --threshold 1`); canonical storage на Mac
      указывает на новую ревизию. Распространение до семян/ноды — штатная сетевая синхронизация
      Radicle (фоновый процесс, см. «Осталось»).
- [x] 3. **Обёртка `rad` для workspace.** `pkgs.lattice.rad-peer` установлен в PATH ACP-сессий
      (`lattice.pi-acp-daemon.path` в node config) и `RAD_HOME` peer-профиля попадает в
      окружение агентов (`lattice.pi-acp-daemon.extraEnv`) — `git push rad://...` через
      radicle remote helper подписывается peer-идентичностью ноды.
- [x] 4. **GitHub deploy key plumbing.** age-секрет `github-lattice-deploy-key` объявлен в node
      config (guarded через `builtins.pathExists`, см. ниже) и
      [secrets.nix](../../nodes/mytecor-homelab/secrets/secrets.nix) (recipients
      admin+node). **Ключ сгенерирован и загружен**: `ssh-ed25519` repo-scoped (read-write)
      для `mytecor/lattice`, публичная часть — `nodes/mytecor-homelab/secrets/github-lattice-deploy-key.pub`,
      закрытая — в `github-lattice-deploy-key.age` (возможно ноде и recovery). Добавлен в GitHub
      через `gh repo deploy-key add … --title lattice-node-dev (f15-02)`; ssh-алиас
      `github-lattice` пишет `lattice-workspace-init`, push `publish` идёт через `git@github-lattice:`.
      Fetch остаётся анонимным (https origin, git-cache-proxy, f9-02).
- [x] 5. **`lattice-workspace-init`** настраивает на ноде `publish` с двумя push URL
      (rad:// + github через алиас) — по рецепту
      [DEPLOYMENT.md](../../DEPLOYMENT.md#публикация-в-radicle-и-github); правило «push только
      через `publish`, оба remote» действует и для ACP-агента. Когда ключ ещё не создан,
      GitHub push URL остаётся анонимным https (push отложен до провижинга ключа),
      ssh-алиас не пишется.
- [x] 6. **Ключевая гигиена.** Ни один шаг не печатает расшифрованные ключи в stdout; в задаче
      фиксируются только пути и статусы. Ключ в agenix (mode 0400), в store не попадает.

## Осталось на живой ноде / оператору (не автоматизируется кодом)

Закрыто в этой сессии: правки развёрнуты на ноду (comin), peer-identity сгенерирован
на ноде (`did:key:z6MkqUjzpiYfDAcjnj2379bYfEk4DdLtWQkyfk7nECn6HyZx`), `rad-peer`
в системном PATH, deploy key загружен в GitHub, workspace на ноде видит новые push URL,
identity-документ `d888fa4` (2 delegate'а, threshold 1) синхронизирован в seed-storage ноды
(подтверждено: `refs/rad/id` → d888fa4, оба delegates видны). Осталось:

1. **E2E acceptance (f15-03)**: из ACP-сессии на ноде doc-правка → commit →
   `git push publish main` → подтверждается один commit в Radicle и GitHub →
   comin применяет. До сих пор пуши делались оператором с Mac.

## Критерий готовности (Definition of Done)

- [ ] `git push publish main` из checkout на ноде публикует один commit и в Radicle, и в GitHub;
      при частичном отказе процедура сверки/повтора из [DEPLOYMENT.md](../../DEPLOYMENT.md) выполнима на ноде. **Код-
      часть готова** (rad-peer, RAD_HOME в env сессий, github-lattice алиас + deploy key
      plumbing + ключ загружен в GitHub + делегирование Radicle (revision d888fa4, 1-of-2)
      оформлено и синхронизировано в seed-storage ноды); остаётся E2E
      acceptance (f15-03).
- [ ] Ключи существуют только как файлы (peer-профиль в `/persist`, agenix); в Git и в
      stdout/контексте агента их значений нет.

## Затрагиваемые файлы / слои

- `flake.nix` — пакет `pkgs.lattice.rad-peer` (обёртка `rad` peer-профиля, RAD_HOME).
- [`profiles/pi/`](../../profiles/pi/README.md) / `lattice.pi-acp-daemon.path` — обёртка в PATH сессий.
- [`modules/pi-acp-daemon/options.nix`](../../modules/pi-acp-daemon/options.nix) — опция `extraEnv`
  (окружение агентов; RAD_HOME).
- [`profiles/node-dev/config.nix`](../../profiles/node-dev/config.nix) — impermanence peer-профиля
  и `/root/.ssh`, передача deploy key в `lattice-workspace-init`.
- [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix) — `radiclePeerHome`,
  guarded agenix-секрет `github-lattice-deploy-key`, RAD_HOME в `extraEnv`, impermanence.
- `nodes/mytecor-homelab/secrets/secrets.nix` — recipients для нового deploy key.
- [`scripts/lattice-workspace-init.sh`](../../scripts/lattice-workspace-init.sh) — ssh-алиас
  `github-lattice` + GitHub push URL через алиас (при наличии ключа).
- [`DEPLOYMENT.md`](../../DEPLOYMENT.md) — операционная процедура публикации с ноды.

## Открытые вопросы

- **Порог подписей Radicle identity**: 1-of-2 (push с ноды автономен; нода = trusted device
  владельца) против 2-of-2 (каждый push требует обеих машин — ломает автономность ноды).
  Рекомендация — 1-of-2.
- **GitHub**: deploy key (repo-scoped, не истекает) vs fine-grained PAT. Рекомендация — deploy key.
