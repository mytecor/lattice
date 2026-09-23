# Node Dev Loop Profile (f15-01, f15-02)

Идемпотентный one-shot-сервис `lattice-workspace-init`, который поднимает и поддерживает
рабочую копию Lattice на ноде (`/var/lib/lattice-workspace/lattice`). ACP-сессии открываются
в ней по умолчанию (`lattice.pi-acp-daemon.defaultCwd`), что позволяет агенту в сессии
править, коммитить и пушить `main` прямо с ноды.

## Что делает сервис

1. Клонирует `main`-ветку Lattice RID из локального Radicle seed storage
   (`/var/lib/radicle/storage/z3AqC22BKQ5Gnrkw49N7PGJa91G6L`), падает на GitHub,
   если seed storage пуст (зеркало политики `lattice-comin-source-sync`).
2. Настраивает remote `publish` с двумя push URL (Radicle + GitHub) по рецепту
   [DEPLOYMENT.md](../../DEPLOYMENT.md#публикация-в-radicle-и-github).
3. `git pull --ff-only` обновляет рабочую копию; грязное дерево не трогает
   (сессия могла оставить правки).

## f15-02: push-доступы с ноды

- **Radicle peer-identity.** Пакет [`pkgs.lattice.rad-peer`](../../flake.nix) — `rad` против
  отдельного peer-профиля `RAD_HOME=/persist/var/lib/radicle-peer` (строго отделён от
  seed-профиля `rad-system`). `lattice.pi-acp-daemon.extraEnv.RAD_HOME` указывает на тот же
  профиль, поэтому `git push rad://...` (radicle remote helper) подписывается peer-
  идентичностью ноды. Профиль persistence сохраняет `/persist/var/lib/radicle-peer`.
- **GitHub deploy key.** Задание
  [f15-02](../../roadmap/f15-node-dev-loop/f15-02-publish-access.md): пока оператор не создал
  `secrets/github-lattice-deploy-key.age`, GitHub push URL остаётся анонимным https (push
  отложен). Как только `.age`-файл появился (recipients admin+node уже в
  `secrets.nix`), `lattice-workspace-init` пишет root ssh-алиас `github-lattice`
  (IdentityFile = расшифрованный agenix-путь) и переводит GitHub push URL на
  `git@github-lattice:mytecor/lattice.git`. `/root/.ssh` персистится через impermanence.

## Границы

- Рабочая копия **строго отдельна** от `/var/lib/comin` (bare source для comin) и
  `/var/lib/radicle/storage` (bare storage seed). Init-скрипт не трогает ни то, ни другое.
- Impermanence: директории `/var/lib/lattice-workspace` и `/root/.ssh` объявлены в
  `environment.persistence."/persist".directories` профиля и переживают reboot.

## Подключение

Профиль подключён в `flake.nix` в `nixosConfigurations.mytecor-homelab` вместе с остальными
профилями. Подключение вручную в node config:

```nix
imports = [ "${profiles}/node-dev/config.nix" ];
```

См. также: [F15 README](../../roadmap/f15-node-dev-loop/README.md),
[f15-01 roadmap task](../../roadmap/f15-node-dev-loop/f15-01-workspace-checkout.md),
[f15-02 roadmap task](../../roadmap/f15-node-dev-loop/f15-02-publish-access.md).