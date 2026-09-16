# Упаковать и закрепить r1s execution backend

Фича: [F10 — disposable worker](./README.md). Зависит от F8 (контейнерный Pi runtime из
[f8-06](../f8-pi-runtime/f8-06-network-acp-daemon.md) / [f10-04](./f10-04-pi-rpc-runner.md)) и
готового [r1s](https://github.com/mytecor/r1s).

## Контекст

Execution path F10 строится поверх r1s: `r1s`-клиент и `r1sd`-allocator должны появляться на ноде
тем же декларативным способом, что и остальные пакеты Lattice, и воспроизводиться из flake lock
state. Это подготовительная задача — она делает r1s доступным как flake-пакет, но не реализует
executor/lifecycle (это f10-03).

## Что сделать

- [x] Закрепить источник r1s (рев и хеши) и зафиксировать требуемый го-тулчейн.
- [x] Добавить пакет в overlay `pkgs.lattice` и экспортировать в `packages.${system}`.
- [x] Зафиксировать совместимый toolchain и upgrade procedure.

## Критерий готовности

- [x] `r1s` и `r1sd` появляются как `packages.${system}.r1s` / `.r1sd` и собираются из flake lock state.

## Затрагиваемые файлы / слои

- `flake.nix`, `flake.lock`
- `packages/r1s/`

## Открытые вопросы

_нет_.

## Реализация

Завершено 2026-09-16. Источник r1s закреплён по commit `b40a279` (SRI-хеш исходников и vendorHash
зафиксированы в `packages/r1s/package.nix`). `go.mod` и зависимость `Reticulum-Go v1.2.0` требуют
`go >= 1.27.1`, поэтому основной пин nixpkgs сдвинут с `3ed67ec` (2026-09-02, `go_1_27 = 1.27.0`)
на `dc5d91f` (2026-09-07, `go_1_27 = 1.27.1`); r1s собирается `buildGoModule.override { go = …go_1_27; }`
из основного пина — отдельного `nixpkgs-go`-инпута нет:

```sh
nix build .#packages.x86_64-linux.r1s
```

Пакет входит в `nix flake check` (eval всех outputs). Полная сборка для `x86_64-linux` выполняется
в GitHub Actions; локально — на x86_64-linux билдере или ноде. `doCheck = true` прогоняет
`go test ./...` r1s в sandbox.

Использование r1s как execution backend (executor contract, lifecycle, запуск worker) — отдельная
задача [f10-03](./f10-03-worker-lifecycle.md).
