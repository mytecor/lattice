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

- [ ] 1. **Radicle peer-identity ноды.** На ноде, в отдельном `RADICLE_HOME`
      (например `/persist/var/lib/radicle-peer`, вне seed-профиля), разово вручную:
      `rad auth` — ключ генерится на месте. Персистентность каталога — через impermanence ноды.
- [ ] 2. **Делегирование в RID Lattice.** `rad id update` добавить ноду как делегата/подписанта
      (подпись — ключом оператора с Mac). Выбрать и зафиксировать порог подписей
      (1-of-2 или 2-of-2, см. «Открытые вопросы»).
- [ ] 3. **Обёртка `rad` для workspace.** Небольшой package, задающий `RADICLE_HOME` peer-профиля,
      чтобы не смешивать seed-профиль `rad-system` и peer-операции; добавить её в PATH ACP-сессий
      (`lattice.pi-acp-daemon.path` / tool profile).
- [ ] 4. **GitHub deploy key.** ssh-ed25519 deploy key (repo-scoped, write) для
      `mytecor/lattice`: закрытый ключ — в agenix (`secrets/github-lattice-deploy-key.age`,
      новый recipient в `secrets.nix` по [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md)); ssh-алиас
      в node config; push URL `publish` на ноде идёт через алиас. Fetch остаётся анонимным
      (git-cache-proxy, f9-02).
- [ ] 5. **`lattice-workspace-init`** настраивает на ноде `publish` с двумя push URL
      (rad:// + github через алиас) — по рецепту
      [DEPLOYMENT.md](../../DEPLOYMENT.md#публикация-в-radicle-и-github); правило «push только
      через `publish`, оба remote» действует и для ACP-агента.
- [ ] 6. **Ключевая гигиена.** Ни один шаг не печатает расшифрованные ключи в stdout; в задаче
      фиксируются только пути и статусы.

## Критерий готовности (Definition of Done)

- [ ] `git push publish main` из checkout на ноде публикует один commit и в Radicle, и в GitHub;
      при частичном отказе процедура сверки/повтора из DEPLOYMENT.md выполнима на ноде.
- [ ] Ключи существуют только как файлы (peer-профиль в `/persist`, agenix); в Git и в
      stdout/контексте агента их значений нет.

## Затрагиваемые файлы / слои

- `packages/` — обёртка `rad` peer-профиля (RADICLE_HOME).
- [`profiles/pi/`](../../profiles/pi/README.md) / `lattice.pi-acp-daemon.path` — обёртка в PATH сессий.
- [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix) — impermanence
  peer-профиля, ssh-алиас, включение.
- `secrets.nix` / `secrets/*.age` — новый deploy key по [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md).
- [`DEPLOYMENT.md`](../../DEPLOYMENT.md) — операционная процедура публикации с ноды.

## Открытые вопросы

- **Порог подписей Radicle identity**: 1-of-2 (push с ноды автономен; нода = trusted device
  владельца) против 2-of-2 (каждый push требует обеих машин — ломает автономность ноды).
  Рекомендация — 1-of-2.
- **GitHub**: deploy key (repo-scoped, не истекает) vs fine-grained PAT. Рекомендация — deploy key.
