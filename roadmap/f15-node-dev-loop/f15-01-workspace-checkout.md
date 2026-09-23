# f15-01. Рабочий checkout Lattice на ноде и defaultCwd для ACP-сессий

Фича: [F15 — Разработка с ноды](./README.md). Опирается на работающий ingress
[f8-06](../f8-pi-runtime/f8-06-network-acp-daemon.md) и Radicle seed/comin из
[f4-01](../f4-payload/f4-01-radicle-seed-comin.md).

## Контекст

ACP-сессии стартуют в `defaultCwd`, который в
[`modules/pi-acp-daemon/config.nix`](../../modules/pi-acp-daemon/config.nix) захардкожен как home
пользователя сервиса (`/root`), а опции в модуле нет: клиент обязан передавать cwd в `session/new`
или работать в `/root`. `/root` не входит в `environment.persistence."/persist".directories` ноды —
всё, что там появится, стирается reboot-ом. Рабочего checkout Lattice на ноде нет: radicle-слой —
только seeder (bare storage для comin), и `comin` читает свой нормализованный bare repository
(`/var/lib/comin/source/repository`), а не рабочую копию.

Нужен персистентный рабочий checkout на ноде, в котором ACP-сессии открываются по умолчанию, —
строго отдельный от source-пути comin (рабочая копия ≠ bare storage нормализатора).

## Что сделать

- [x] 1. **Опция `lattice.pi-acp-daemon.defaultCwd`** (`types.str`, `mkDefault userHome`) в
      [`modules/pi-acp-daemon/options.nix`](../../modules/pi-acp-daemon/options.nix); сгенерированный
      `defaultCwd` конфига Hydra в
      [`modules/pi-acp-daemon/config.nix`](../../modules/pi-acp-daemon/config.nix) берёт её, а не
      `userHome` напрямую.
- [x] 2. **Контракт-тест** в [`tests/pi-acp-daemon.nix`](../../tests/pi-acp-daemon.nix):
      значение из node config попадает в generated config Hydra. Membership-проверка (value in
      file), не equality — изменение дефолта node config не должно ломать тест.
- [x] 3. **Workspace на ноде**: `/var/lib/lattice-workspace` (mode 0700, root) + запись в
      `environment.persistence."/persist".directories`
      [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix) — без impermanence
      checkout переживёт только до reboot. (Профиль [`profiles/node-dev`](../../profiles/node-dev/README.md)
      объявляет директорию в persistence своей секцией `environment.persistence`.)
- [x] 4. **Идемпотентный one-shot `lattice-workspace-init`**: клон RID Lattice из локального seed
      storage (`/var/lib/radicle/storage/<RID>`, GitHub fallback), checkout `main` в
      `/var/lib/lattice-workspace/lattice`, настройка remote `publish` по рецепту из
      [DEPLOYMENT.md](../../DEPLOYMENT.md#публикация-в-radicle-и-github). Сервис не мешает
      существующему checkout (уже склонировано — не трогает), не создаёт симлинков в
      `/var/lib/comin` и `/var/lib/radicle/storage`.
- [x] 5. **Node config**: `lattice.pi-acp-daemon.defaultCwd = "/var/lib/lattice-workspace/lattice";`
      — новые `session/new` без явного cwd открываются в checkout.
- [x] 6. **Документация**: раздел «Рабочий checkout на ноде» в
      [DEPLOYMENT.md](../../DEPLOYMENT.md): workspace — рабочая копия для правок и пуша, comin
      читает свой bare source; путь для `session/new` извне — только workspace.
- [x] 7. **Серверная политика cwd** (`packages/hydra-acp/package.nix`): демон принимает
      клиентский cwd как есть (`Wi = K.object({cwd: K.string(), ...})` — обязательное поле),
      `manager.create` спавнит агента ровно с этим путём, `defaultCwd` в WS `session/new` не
      консультируется вовсе — поэтому stateless-клиент (acp-ui, Ferngeist) увёл бы сессию из
      checkout (пустой путь, `/`, либо иной каталог). Патч: `manager.create` **безусловно**
      заменяет cwd на `fe(this.defaultCwd)` (та же `expandHome`, что использует
      `resolveResurrectTarget`) — любая сессия открывается в рабочем checkout, каким бы путём
      клиент ни пытался её направить. Клиентский патч acp-ui (default workspace в
      `patch-main-ts.mjs`) при этом не нужен: политика задаётся только сервером, и он был
      отменён. Патч **симметричен для чтения**: обработчик `session/list` тоже **безусловно**
      фильтрует по `fe(e.manager.defaultCwd)`, игнорируя cwd из запроса клиента — иначе
      лист-фильтр демона (`Td`/`wo` путь-равенство в `manager.list`) скрыл бы все сессии
      workspace при запросе с чужим путём (например `/`, что acp-ui шлёт после обновления
      страницы).

## Критерий готовности (Definition of Done)

- [x] Новая ACP-сессия стартует в `/var/lib/lattice-workspace/lattice`; каталог,
      checkout и `/persist`-запись переживают reboot ноды. Сервер безусловно заменяет cwd на
      `defaultCwd` (патч `packages/hydra-acp/package.nix`): какой путь ни прислал бы клиент в
      `session/new` (пустой, `/`, произвольный каталог) — сессия открывается в checkout.
- [x] `session/list` возвращает эти сессии даже когда клиент запрашивает список с чужим cwd
      (`/` — как acp-ui после обновления страницы): листинг и создание ведётся по одному
      `defaultCwd`. Регрессия закреплена в [`tests/acp-ingress-smoke.mjs`](../../tests/acp-ingress-smoke.mjs)
      и [`tests/hydra-acp-smoke.mjs`](../../tests/hydra-acp-smoke.mjs).
- [ ] `nix flake check --all-systems --no-build` зелёный, включая новую проверку defaultCwd;
      `/var/lib/comin` и `/var/lib/radicle` не изменили роль (comin source не тронут).

## Затрагиваемые файлы / слои

- [`modules/pi-acp-daemon/`](../../modules/pi-acp-daemon/README.md) — опция + generated config.
- [`tests/pi-acp-daemon.nix`](../../tests/pi-acp-daemon.nix) — контракт-тест.
- [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix) — workspace,
  impermanence, defaultCwd.
- [DEPLOYMENT.md](../../DEPLOYMENT.md) — раздел о рабочем checkout на ноде.
- [`ROADMAP.md`](../../ROADMAP.md) — статус F15.

## Открытые вопросы

_нет_ (размещение `/var/lib/lattice-workspace` + impermanence и отказ от симлинков в comin-source
зафиксированы в [README фичи](./README.md)).
