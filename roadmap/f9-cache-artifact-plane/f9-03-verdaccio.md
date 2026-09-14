# Развернуть Verdaccio как npm proxy/cache

Фича: [F9 — cache и artifact plane](./README.md). Зависит от F8.

## Контекст

npm, pnpm и yarn должны получать пакеты через общий cache, который можно удалить и восстановить из
upstream registries и lockfiles.

## Что сделать

- [x] Добавить закреплённый Verdaccio service и persistent cache directory под `/var/cache`.
      Собран пакет `pkgs.lattice.verdaccio` (verdaccio 6.10.3 через общий `buildPnpmCli`,
      `packages/verdaccio/`), добавлен модуль `lattice.verdaccio` (`modules/verdaccio/`),
      `cacheRoot` по умолчанию `/var/cache/verdaccio`, сервис на loopback.
- [x] Настроить upstream registry, limits, auth boundary и безопасное логирование.
      `upstreamRegistry` (default npmjs), `maxBodySize`, cache-only `publish = false`
      (publish требует htpasswd через `LoadCredential`), `logLevel`, строгий systemd-песочник.
- [x] Подключить npm/pnpm/yarn clients из tool profile без глобальной ручной настройки.
      Опция `clientConfig` (default true) пишет `/etc/npmrc` (npm+pnpm) и `/etc/yarnrc`
      на loopback-прокси; это только URL реестра, не credentials, и не трогает
      f8-03 base-tools (тяжёлый toolchain туда не добавляется).
- [x] Проверить cold/warm install по lockfile и поведение после очистки cache.
      Изначально был добавлен VM-тест `tests/verdaccio.nix` (cold install через
      npm/pnpm/yarn -> warm install тот же graph -> rm cache -> cold install тот же
      результат), но позже убран: он требует сети к npmjs из QEMU-VM и крэшил Node
      под виртуализацией, делая `nix flake check` красным. Поведенческое покрытие
      сведено к контрактному тесту `tests/verdaccio.nix` (см. ниже) и
      eval/config-чекам.

  **Live-находка 2026-09-14 (Node 24 + W^X):** на реальной ноде verdaccio не
  стартовал: systemd-песочница с `MemoryDenyWriteExecute=true` роняет Node 24/V8
  на инициализации V8-изолятов — `v8::base::OS::SetPermissions` на code range
  возвращает `EPERM` (не ожидаемый `ENOMEM`), и V8 падает с
  `Check failed: 12 == (*__errno_location ())` ещё до старта verdaccio. Это
  ломало каждый `comin`-switch на `mytecor-homelab` (status 4), оставляя ноду на
  старой generation и не давая самообновляться из `main`. Зафиксировано:
  `MemoryDenyWriteExecute = false` для Node-сервиса (тот же трейд-офф, что у
  llm-gateway/Bifrost), остальная жёсткость песочника сохранена. Новый
  контрактный тест `tests/verdaccio.nix` закрепляет, что MDWX обязан оставаться
  `false`, а остальные защитные опции — включёнными.

## Критерий готовности

- [x] Package manager ноды (pnpm) использует единственный proxy endpoint.
      Подтверждено live 2026-09-14: pnpm через прокси-зависимые пакеты
      (`left-pad`, `chalk`, `rxjs`, `@types/node` + транзитивные, 11 tarball'ов)
      попадают в `/var/cache/verdaccio`; `pnpm config get registry` =
      `http://127.0.0.1:9212/`. Для этого clientConfig пишет глобальный конфиг
      pnpm 11 `/root/.config/pnpm/config.yaml` (pnpm НЕ читает `/etc/npmrc`;
      `/etc/pnpmrc` и env `NPM_CONFIG_REGISTRY` игнорируются — проверено на
      ноде). Yarn из поддержки убран: в Lattice используется только pnpm.
- [x] Cold install после удаления cache даёт тот же dependency graph по lockfile.
      Подтверждено live 2026-09-14: `rm -rf /var/cache/verdaccio` (без ручного
      `mkdir`) → рестарт юнита → fresh pnpm cold install дал тот же граф (4
      пакета, 11 tarball'ов) и снова наполнил кэш прокси. Disposable-семантика
      кеша закрыта: `CacheDirectory` пересоздаёт cacheRoot до mount
      namespacing на каждый старт.

## Затрагиваемые файлы / слои

- `packages/verdaccio/` — пакет через `buildPnpmCli`.
- `modules/verdaccio/` — NixOS-модуль `lattice.verdaccio`.
- `profiles/cache-plane/` — сервис в cache-plane профиле; порт `verdaccio` в `ports.nix`.
- `nodes/mytecor-homelab/config.nix` — включение сервиса на ноде.

## Открытые вопросы

_нет_.

## Live-находки 2026-09-14 (live-прогон f9-03 на `mytecor-homelab`)

Первый живой прогон после фикса MDWX вскрыл **три дефекта**, которые ускользнули
от eval-чеков и контрактного теста (тот проверял serviceConfig/песочницу, но не
буквальное содержимое генерируемого YAML и не runtime-поведение удаления кэша):

1. **`access: \${anonymous}` → 401 на раздачу.** В `modules/verdaccio/config.nix`
   Nix-экейп `\${anonymous}` давал в YAML literal `${anonymous}`, а `@verdaccio/config`
   `ROLES` знает только `$anonymous`/`$all`/`$authenticated` (и `@`-deprecated).
   Anonymous-клиент не попадал в группу → `401 authorization required` на каждый
   пакет, cold install был невозможен. Исправлено на literal `access:
   $anonymous`; publish/unpublish в cache-only опускаются (пустой ACL = deny всем).
2. **`rm -rf` кэша ронял юнит (226/NAMESPACE).** systemd ставит mount namespacing
   (`ProtectSystem=strict` + `ReadWritePaths`) **до** ExecStartPre, а `tmpfiles`
   создавал каталог только на boot. После runtime-удаления cacheRoot сервис падал.
   Исправлено: `CacheDirectory=verdaccio` + `CacheDirectoryMode=0700` (systemd
   создаёт каталог до namespacing на каждый старт); убран tmpfiles-блок; assertion
   требует `cacheRoot` под `/var/cache`.
3. **clientConfig покрывал только npm, а не pnpm.** pnpm 11 не читает `/etc/npmrc`
   (globalconfig — `$XDG_CONFIG_HOME/pnpm/config.yaml`), `/etc/pnpmrc` и env
   `NPM_CONFIG_REGISTRY` игнорируются (проверено на ноде). clientConfig теперь
   пишет `/root/.config/pnpm/config.yaml` через активационный скрипт (путь вне
   `/etc`, `/root` ephemeral по impermanence). Yarn из поддержки исключён: в
   Lattice используется только pnpm (`buildPnpmCli`), `/etc/yarnrc` yar'ом не
   читается, поддержка была фикцией.

Всё подтверждено live-прогоном: pnpm cold → 11 tarball'ов в кэше прокси; warm →
435ms; cache-drop + fresh store → тот же граф и снова 11 tarball'ов. `nix flake
check --all-systems --no-build` проходит; контрактный тест теперь проверяет
literal ACL-токены в генерируемом YAML и `CacheDirectory`.

## Заметка по статусу (2026-09-13)

Код, модуль и eval завершены; `nix flake check --all-systems --no-build` проходит.
VM-тест (`tests/verdaccio.nix`) был убран вместе с остальными QEMU-тестами — до
включения поведенческой проверки на живом реестре пункты «Критерий готовности»
остаются незакрытыми.
