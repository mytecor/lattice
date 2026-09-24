# acp-web package

Собирает Lattice web-клиент ACP — воркбенч
[acp-components](https://github.com/zvzuola/acp-components) (React + framework-agnostic
core, MIT) как статический SPA, который в LAN/Mesh раздаёт Caddy (контракт
`http://acp-ui.<node>.local` и `https://acp-ui.<meshDomain>` из
[f13-01](../../roadmap/f13-acp-web-client/f13-01-deploy-acp-components.md)). Содержимое — это
`dist/` демо `examples/demo`: `index.html` + ассеты, SPA-fallback на `index.html`.

## Источник и внесение изменений

Исходник **vendored прямо в репозиторий**: каталог [`src/`](./src) — это срез upstream
[`zvzuola/acp-components`](https://github.com/zvzuola/acp-components) на закреплённом коммите:

- upstream-коммит `1708c20274c9f15ee3a072009e5ca9fd3b71a9de`
  (merge PR #1 `hafbit/agent/update-codex-acp`), лицензия MIT;
- срез сделан `git archive <commit>` — ровно tracked-файлы upstream, без `.git`/`node_modules`;
- от upstream срез отличается **только** файлом `pnpm-workspace.yaml` — поверх положена
  Lattice-копия (см. ниже), всё остальное байт-в-байт равно upstream.

### Почему vendored, а не fetchFromGitHub

Vendoring убирает зависимость сборки от сети и от структуры upstream: не нужно обновлять fetch
hash, точный исходник лежит в репозитории и аудируется обычным `git diff`. Обновление upstream —
это осознанная правка в дереве: заменить содержимое `src/` новым `git archive` и зафиксировать
в истории.

Важный нюанс для FOD-стора: `fetchPnpmDeps` использует `src` для `pnpm install
--frozen-lockfile` и заканчивается на целостность по хешу всего дерева. Поэтому дерево, которое
видит `fetchPnpmDeps`, должно оставаться байт-в-байт таким же, как у известного-good стора:
`src = runCommand` в [package.nix](./package.nix) копирует [./src](./src) **как есть**, включая
pristine `examples/demo/src/main.tsx`. Lattice-правка демо-агента применяется **на этапе сборки**
в `postPatch` через [`patch-main-ts.mjs`](./patch-main-ts.mjs) (см. ниже) — после того, как
`fetchPnpmDeps` уже вычислил свой store. Так хеш `fetchPnpmDeps`
(`sha256-TwS7s8OqWKfPcbMzqCuPtfY1GZPYtEHN3WewSc3+0kA=`) и сам offline-store остаются неизменными.

### Lattice-настройки внутри пакета

- **`src/pnpm-workspace.yaml`** — Lattice-копия upstream workspace с ослабленными
  supply-chain политиками pnpm 11 (`minimumReleaseAge`, `blockExoticSubdeps`). Для Nix-pinned
  lock, чей граф и целостность зафиксированы `fetchPnpmDeps`, эти гейты избыточны и во время
  сборки тянут реестр (офлайн-нода падает на EAI_AGAIN). Это единственный файл, которым срез в
  `src/` отличается от pristine upstream, — он уже лежит на своём месте в дереве, дополнительный
  `postPatch`-overlay не нужен.
- **`patch-main-ts.mjs`** — build-time правка `examples/demo/src/main.tsx`: предконфигурированный
  дефолтный ACP-агент. Endpoint деривируется из serving-имени (`acp-ui.<host>` → sibling
  `acp.<host>`, схема `wss://` на https / `ws://` на http), есть build-time
  `VITE_ACP_ENDPOINT`-override; агент несёт `clientInfo` (без него
  `hydra-acp 0.1.183` zod-схема отклоняет `clientInfo: null` в initialize). Применяется в
  `postPatch`, чтобы не трогать дерево, которое видит `fetchPnpmDeps`. Скрипт ключуется на
  точный upstream-блок и падает loudly при изменении формы.

## Сборка

Сборка [package.nix](./package.nix) — полный workspace-билд: `pnpm install --offline
--frozen-lockfile` против FOD-стора из `fetchPnpmDeps` (fetcherVersion 4), затем `pnpm build`
(core+react) и `pnpm --filter @acp-components/demo build`. Выход — `dist/` демо.

`pnpmConfigHook` намеренно не используется: в песочнице NixOS на целевой ноде его
реконструкция SQLite-индекса v11 не срабатывала (pnpm считал offline-store пустым и уходил в
сеть). Процедура из `configurePhase` (извлечение `pnpm-store.tar.zst`, восстановление
`v11/index.db` из `.sql`-дампа, arch/platform, store-dir, `pnpm install --offline`) воспроизводит
логику хука вручную и проверена на полном reuse FOD-стора с нулём сетевых обращений.

## Проверка

```sh
nix flake check --all-systems --no-build   # eval всех outputs + контракт-тесты
nix build .#packages.x86_64-linux.acp-web  # на билдере/ноде x86_64-linux или в CI
```

Контракт раздачи покрыт [tests/app-services.nix](../../tests/app-services.nix): LAN-сайт
`acp-ui.<node>.local` отдаёт SPA из store-path пакета `acp-web`, mesh-виртуалхост делит
`extraConfig`, mdns-юниты активны.
