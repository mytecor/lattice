# Собрать `profiles/base` и подключить в `nodes/example`

Фича: [F1 — одна железная нода](./README.md).

## Контекст

`profiles/base` объединяет GitOps, общий unfree-предикат и базовые системные дефолты. Итоговая
модель сборки зафиксирована в [ARCHITECTURE.md](../../ARCHITECTURE.md): профиль подключается
корневым flake из общего `profiles` input с `flake = false`.

## Что сделать

- [x] Создать `profiles/base/` по образцу остальных профилей.
- [x] Подключить в `base` профиль `gitops`, unfree-предикат и базовые системные дефолты.
- [x] Подключить `profiles/base` к `nixosConfigurations.example` в корневом flake.
- [x] Добавить воспроизводимую `x86_64-linux` проверку system closure в flake и CI.
- [x] Проверить, что профили действительно попадают в сборку (`nixos-rebuild build`).

### Как закрыт последний пункт (2026-09-16)

Профили реально попадают в сборку, подтверждено двумя путями:

1. **CI (`checks.x86_64-linux.example`)** — `example` строится как toplevel через
   `tests/default.nix`; asserts неявно проверяли присутствие `profiles/base`
   (`services.comin.enable`, `nix.settings.auto-optimise-store`, `nix.gc.automatic`).
   Теперь эта группа сделана явной и задокументированной как регрессионный guard
   f1-01 (см. комментарий в `tests/default.nix`).
2. **Живая нода `mytecor-homelab`** — comin применяет `main` (последний применённый
   commit — нормализация `4e80cd2`, текущий `main`) и собирает/переключает system с
   профилями. В `current-system` присутствуют маркеры `profiles/base`:
   `comin.service`, `lattice-comin-source-sync.{service,timer}`; runtime
   `auto-optimise-store = true`; `nix-gc.timer` запланирован; root эфемерный
   (btrfs `@root`, `x-initrd.mount`), `/etc/nixos` пуст.

## Критерий готовности

- [x] `profiles/base` существует и собирается.
- [x] `nodes/example` собирается с подключённым профилем.

## Затрагиваемые файлы / слои

- `profiles/base/` (новый)
- Корневой `flake.nix`, `nodes/example/default.nix`
- `DEPLOYMENT.md`, `profiles/README.md` (упоминания `base`)

## Открытые вопросы

Pure evaluation (`nix flake check --all-systems --no-build`) проходит. Полную сборку
`checks.x86_64-linux.example` выполняет GitHub Actions; локально она требует `x86_64-linux`
builder или запуска на целевом N100, потому что локальный host — `aarch64-darwin`.
Закрыто 2026-09-16: сборку подтверждает CI и живая нода (см. «Как закрыт последний пункт»).
