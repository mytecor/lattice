# Pi runtime tool profile (F8)

Профиль определяет **воспроизводимый контракт tools** для Pi-рантайма (f8-03): один базовый набор
`bash/git/tools`, который получает и интерактивная нода (`lattice.pi` из
[`modules/pi`](../../modules/pi/README.md)), и будущий worker, и интерактивный devShell
разработчика.

## Что здесь

- [`base-tools.nix`](./base-tools.nix) — единый источник базового набора пакетов
  (`bash`, `git`, shell-инструменты, сетевые клиенты, редактор/поиск). Импортируется из
  `modules/pi/config.nix` и из `flake.nix` (devShell + пакет `pi-tool-profile`), поэтому нода и
  окружение разработчика гарантированно видят одинаковый набор.
- `environment.etc."pi.env"` — inspectable-контракт окружения: `PATH` из tool profile, `LANG`/
  `LC_ALL=C.UTF-8`, git identity boundary (`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=…/.gitconfig`).

## Принципы

- **Не тянуть случайные user/global пакеты.** На ноде tools попадают в `systemPackages` ровно из
  декларации tool profile (`lattice.pi.tools` поверх базы); user/global-установки не используются.
- **Расширение не трогает рантайм.** Новый проект добавляет свой toolchain через
  `lattice.pi.tools = [ "nodejs" "go" ]` (на ноде) или через `pkgs.mkShell { inputsFrom =
  [ pkgs.lattice.pi-develop-shell ]; packages = [ pkgs.nodejs ]; }` (в разработке), не меняя
  `modules/pi`/`profiles/pi`.
- **Один контракт для TUI и worker.** Базовый набор (`base-tools.nix`) общий; RPC-режим F10
  получает его же через tool profile.

## Расширение на ноде

```nix
# nodes/<node>/config.nix
lattice.pi = {
  enable = true;
  tools = [ "nodejs" "curl" ];
};
```

## Smoke-проверка

Полный smoke check базового контракта из чистого окружения выполняется тестом
[`tests/pi-tool-profile.nix`](../../tests/pi-tool-profile.nix) (входит в `nix flake check`):

- в store проверяется наличие бинарников базового набора и дополнительных tools;
- из чистого окружения (`env -i`) запускаются ключевые команды: `git`, `bash`, `curl`, `jq` и т.д.;
- подтверждается фиксация `PATH`/locale/git-identity контракта.

Локально (нужен `x86_64-linux` builder, как и остальные полные проверки):

```sh
nix build .#checks.x86_64-linux.pi-tool-profile
```
