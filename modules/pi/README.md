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
  `defaultModel`, `defaultThinkingLevel`, `theme`.
- `lattice.pi.models` — содержимое `models.json`: attrsOf providers. У каждого
  provider есть `baseUrl`, `api`, `apiKey` (nullable), `discoverModels`,
  `models` (explicit logical классы) и `modelOverrides`.

## Граница секретности

Секреты в Nix store не попадают. `apiKey`/`headers` подаются как env/command ссылки
(`"$VAR"`, `"!cmd"`) либо вообще отсутствуют — в этом случае Pi выводит auth из
`/login`/`auth.json`. Для f8-02 (Pi подключается к gateway без client auth по loopback)
credentials в конфиге нет; upstream provider keys остаются только внутри `llm-gateway`.

Имя npm-пакета, версия, lock-файл и сборка принадлежат [`packages/pi`](../../packages/pi/README.md).
Общий механизм сборки pnpm CLI находится в
[`packages/pnpm-cli-builder`](../../packages/pnpm-cli-builder/README.md).
