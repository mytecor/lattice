# F15. Разработка с ноды (node dev-loop)

Полный цикл работы над Lattice прямо с ноды через ACP: ACP-сессия (Ferngeist / acp-ui) открывается
в рабочем checkout Lattice на ноде, агент коммитит и публикует `main` одновременно в Radicle и
GitHub (общий remote `publish` по правилу из [DEPLOYMENT.md](../../DEPLOYMENT.md)), а нода
применяет коммит через штатный `comin` — на том же железе, где работает её собственная ACP-сессия.

До этой фазы нода умеет только получать `main` (seed + comin из f4-01) и обслуживать ACP-сессии
(f8-06), но сессии стартуют в `/root` (хардкод `defaultCwd` в
[`modules/pi-acp-daemon/config.nix`](../../modules/pi-acp-daemon/config.nix)) без рабочего
checkout и без прав публикации: push в Radicle требует peer-identity ноды, push в GitHub —
credential.

[f15-01](./f15-01-workspace-checkout.md) реализован: сессии открываются в рабочем checkout
`/var/lib/lattice-workspace/lattice` (`lattice.pi-acp-daemon.defaultCwd`), which создаёт профиль
[`profiles/node-dev`](../../profiles/node-dev/README.md) через one-shot `lattice-workspace-init`.
[f15-02](./f15-02-publish-access.md) — кодовая часть готова: `pkgs.lattice.rad-peer` (peer-
identity ноды, RAD_HOME=/persist/var/lib/radicle-peer), `lattice.pi-acp-daemon.extraEnv.RAD_HOME`
(remote helper rad:// подписывается peer-ключом), guarded agenix-секрет `github-lattice-deploy-key`
+ ssh-алиас `github-lattice` в `lattice-workspace-init` (GitHub push через `git@github-lattice:`),
impermanence. [f15-03](./f15-03-dev-loop-acceptance.md) — acceptance закрыт 2026-09-24:
из ACP-сессии на ноде сделан doc-правка → commit → `git push publish main` → тот же commit в
Radicle и GitHub → comin применил; нативная проверка flake прошла. Рабочая копия, ключи и опция cwd
закрывают dev-loop; они же переиспользуются контейнерным Pi runtime из
[f10-04](../f10-disposable-worker/f10-04-pi-rpc-runner.md), поэтому работа не выбрасывается при
переходе к F10.

Задачи: [f15-01](./f15-01-workspace-checkout.md),
[f15-02](./f15-02-publish-access.md), [f15-03](./f15-03-dev-loop-acceptance.md).

- [ ] **Критерий готовности закрыт 2026-09-24** (acceptance f15-03): из ACP-сессии на ноде коммит
  доезжает до Radicle и GitHub одним `git push publish main` и применяется нодой через comin;
  checkout, ключи и конфигурация переживают reboot (impermanence); `nix flake check --no-build`
  гоняется нативно на ноде (`all checks passed!`).

**Осознанно откладываем (до F…):** возврат `pi-acp-daemon` к strict sandbox —
запись 5 [BACKLOG.md](../BACKLOG.md) (не специфика этой фичи); контейнерная execution boundary —
[F10](../f10-disposable-worker/README.md); auth на ACP endpoint — запись 4
[BACKLOG.md](../BACKLOG.md); мульти-воркспейсы и несколько checkout'ов на ноде — по потребности
после подтверждения одного цикла; запуск публикаций не-интерактивным агентом без человека —
[F11](../f11-controller/README.md).
