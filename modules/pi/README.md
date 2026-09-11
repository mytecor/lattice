# Pi coding agent

Модуль предоставляет `lattice.pi.enable` и декларативную конфигурацию Pi.

При включении он:

1. добавляет собранный `pkgs.lattice.pi` в системный профиль;
2. генерирует `~/.pi/agent/settings.json` и `~/.pi/agent/models.json` из Nix-опций
   как immutable JSON в Nix store и материализует их **симлинками** для целевого
   пользователя (`lattice.pi.user`, по умолчанию `root`).

Каталог `~/.pi/agent` остаётся writable: на store-файлы ссылаются только
`settings.json`/`models.json`, а runtime-состояние Pi (sessions, trust, ключи `/login`)
пишется рядом и не переживает перезагрузку или пересборку как immutable данные.

## Опции

- `lattice.pi.enable` — добавить Pi в системный профиль и материализовать конфиг.
- `lattice.pi.user` — целевой пользователь для `~/.pi/agent` (по умолчанию `root`).
- `lattice.pi.settings` — содержимое `settings.json`: `defaultProvider`,
  `defaultModel`, `defaultThinkingLevel`, `theme`, `packages` и `extensions`.
  `packages` — pi packages (строка-спека `npm:`/`git:` с закреплённой версией/рефом
  либо Nix-пакет, store-path). `extensions` — прямые пути к файлу/каталогу
  расширения; для Nix-сборок модуль раскрывает пакет-значение в `"${p}/extension"`
  (см. [`packages/pi-mcp-adapter`](../../packages/pi-mcp-adapter/README.md)),
  например `extensions = [ pkgs.lattice.pi-mcp-adapter ];`.
- `lattice.pi.models` — содержимое `models.json`: attrsOf providers. У каждого
  provider есть `baseUrl`, `api`, `apiKey` (nullable), `discoverModels`,
  `models` (explicit logical классы) и `modelOverrides`.
- `lattice.pi.tools` (f8-03) — дополнительные tools поверх базового контракта.
  Значения — имена атрибутов `pkgs` или package-значения; итог попадает в
  `environment.systemPackages` как часть воспроизводимого tool profile.
- `lattice.pi.envContract` (f8-03) — генерировать `/etc/pi.env` с контрактом
  окружения (PATH из tool profile, locale, git identity boundary). Включено по
  умолчанию через profile.
- `lattice.pi.toolProfile` (read-only) — итоговая derivation tool profile
  (базовый контракт из [`profiles/pi/base-tools.nix`](../../profiles/pi/base-tools.nix)
  + `tools`), на которую ссылается `environment.systemPackages`.

## Базовый контракт tools (f8-03)

Единый источник базового набора `bash/git/tools` — [`profiles/pi/base-tools.nix`](../../profiles/pi/base-tools.nix):

- импортируется отсюда (`modules/pi/config.nix`) и из `flake.nix` (devShell + пакет `pi-tool-profile`),
  поэтому нода и окружение разработчика видят одинаковый набор;
- нода получает tools в `systemPackages` ровно из декларации (базовый контракт + `lattice.pi.tools`),
  без зависимости от случайных user/global пакетов;
- расширение новым проектом не трогает рантайм: `lattice.pi.tools = [ "nodejs" … ]` на ноде либо
  `pkgs.mkShell { inputsFrom = [ pkgs.lattice.pi-develop-shell ]; }` в разработке.

Окружение фиксируется в `/etc/pi.env` (read-only, inspect-only): `PATH` из tool profile,
`LANG`/`LC_ALL=C.UTF-8`, git identity boundary (`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=…/.gitconfig`).

Smoke check из чистого окружения — [`tests/pi-tool-profile.nix`](../../tests/pi-tool-profile.nix)
и NixOS-проверки в [`tests/pi-config.nix`](../../tests/pi-config.nix).

## Граница секретности

Секреты в Nix store не попадают. `apiKey`/`headers` подаются как env/command ссылки
(`"$VAR"`, `"!cmd"`) либо отсутствуют — в этом случае Pi выводит auth из `/login`/`auth.json`.
Реальные credentials всегда остаются вне store: для f8-02 они живут только внутри `llm-gateway`
(agenix-секреты upstream провайдеров).

Особый случай — keyless loopback gateway без client auth: сама Pi считает провайдера пригодным
только при непустом `apiKey` (иначе список доступных моделей пуст и `session/new` завершается
`authRequired`). Поэтому для такого провайдера полагается **несекретный placeholder-literal**
(например `lattice-loopback-gateway` на `mytecor-homelab`) — gateway игнорирует Bearer,
в store не попадает ничего секретного, а `pi --terminal-login` на ноде не требуется.

Имя npm-пакета, версия, lock-файл и сборка принадлежат [`packages/pi`](../../packages/pi/README.md).
Общий механизм сборки pnpm CLI находится в
[`packages/pnpm-cli-builder`](../../packages/pnpm-cli-builder/README.md).
