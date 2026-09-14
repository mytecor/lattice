# F9. Cache и artifact plane

Долгоживущие ускорители отделяются от ценных результатов. Git mirrors, npm packages и Nix
nar-файлы являются disposable caches на локальной POSIX FS; artifacts, screenshots, logs, builds,
backups и test outputs публикуются как объектные данные с явной ссылкой из результата задачи.

Зависит от source bootstrap [f4-01](../f4-payload/f4-01-radicle-seed-comin.md),
[F7](../f7-llm-gateway/README.md) и [F8](../f8-pi-runtime/README.md). Соответствует
[вехе 9](../../ROADMAP.md#f9-caches-и-artifacts).

Задачи: [f9-01](f9-01-git-cache-proxy.md),
[f9-02](f9-02-git-repository-access.md),
[f9-03](f9-03-verdaccio.md).

**Статус:** f9-01 (Git cache proxy как NixOS-сервис) и f9-02 (repo-scoped
authorization) выполнены 2026-09-12. Git cache proxy развёрнут на homelab с
`allowRepos = [ "mytecor/lattice" ]`; модульный assertion запрещает upstream
credential без непустого allowlist.

2026-09-13: реализован f9-03 (Verdaccio).

- **f9-03 Verdaccio** — упакован `pkgs.lattice.verdaccio` (6.10.3 через
  `buildPnpmCli`) и добавлен модуль `lattice.verdaccio` (`modules/verdaccio/`):
  loopback cache-only proxy, `cacheRoot` `/var/cache/verdaccio`, `publish`
  по умолчанию выключен, строгий systemd-песочник. Опция `clientConfig` пишет
  `/etc/npmrc` + `/etc/yarnrc` на loopback-прокси для npm/pnpm/yarn ноды.

Eval всех чеков проходит локально (`nix flake check --all-systems --no-build`);
VM-тесты (QEMU) из репозитория убраны — их место в CI занимали микро-бут, но они
делали `nix flake check` красным (Node crash под QEMU, хрупкие runtime-assertion),
поэтому поведенческое покрытие сведено к evaluation/config-чекам и Rust unit-тестам.

**Критерий готовности:** Pi получает ускорение Git/npm/Nix из локальных caches, артефакт
публикуется и читается по immutable reference, а удаление любого cache влияет только на время
следующего выполнения. Private Git objects не выдаются клиенту без repo-scoped authorization.

**Не входит:** shared POSIX filesystem между workers и S3-FUSE. Выбор конкретного S3-compatible
хранилища не должен менять artifact contract.
