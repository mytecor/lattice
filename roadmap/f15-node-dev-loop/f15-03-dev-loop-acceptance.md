# f15-03. Acceptance: полный цикл «правка → публикация → deploy» из ACP-сессии

Фича: [F15 — Разработка с ноды](./README.md). Зависит от
[f15-01](./f15-01-workspace-checkout.md) (workspace, cwd) и
[f15-02](./f15-02-publish-access.md) (push-доступы).

## Контекст

Финальная проверка фичи: агент в ACP-сессии на ноде доводит правку до применённой конфигурации ноды
без участия Mac. Заодно проверяются отказоустойчивость (частичный push, рестарт демона) и
нативный `x86_64-linux` builder — проверка, которой нет на Mac.

## Что сделать

- [ ] 1. **E2E из клиента ACP** (Ferngeist или acp-ui): doc-правка в checkout на ноде → commit →
      `git push publish main` → подтверждено появление одного и того же commit в Radicle и GitHub.
- [ ] 2. **GitOps-замыкание**: `lattice-comin-source-sync` выбрал локальный Radicle head
      (приоритет Radicle), `comin` переключил ноду на новый commit; версия на ноде соответствует.
- [ ] 3. **Сбой частичной публикации**: push, отклонённый одним remote, обнаруживается сверкой
      `refs/heads/main` и повторяется по [DEPLOYMENT.md](../../DEPLOYMENT.md); расхождение
      разрешается без ручного вмешательства в нормализатор.
- [ ] 4. **Живучесть**: рестарт `pi-acp-daemon` и reboot ноды не теряют checkout, peer-ключ
      (`/persist/var/lib/radicle-peer`) и deploy key; после reboot `lattice-workspace-init`
      идемпотентен.
- [ ] 5. **Нативная проверка flake на ноде**: `nix flake check --all-systems --no-build` из
      checkout выполняется на x86_64-linux нативно; результат зафиксирован в этой задаче.
- [ ] 6. **Документация**: раздел «Рабочий checkout на ноде» в
      [DEPLOYMENT.md](../../DEPLOYMENT.md) (границы workspace vs comin source, правило `publish`,
      ручные шаги f15-02) — фактическая процедура сверена с живой нодой.

## Критерий готовности (Definition of Done)

- [ ] Полный цикл «правка из ACP-сессии → commit → `git push publish main` → оба remote →
      comin → switch на ноде» воспроизведён; после reboot рабочее состояние восстановилось само.
- [ ] Документация описывает dev-loop как рабочий способ работы над Lattice с ноды; исключение —
      `/var/lib/comin` и `/var/lib/radicle/storage` не используются как cwd сессий.

## Затрагиваемые файлы / слои

- [`DEPLOYMENT.md`](../../DEPLOYMENT.md), [`README.md`](../../README.md) — документация dev-loop.
- [`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix) — правки по итогам
  acceptance, если вскроются.
- [`ROADMAP.md`](../../ROADMAP.md) — закрытие F15.

## Открытые вопросы

_нет_
