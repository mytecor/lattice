# Развернуть и проверить Attic

Фича: [F9 — cache и artifact plane](./README.md). Зависит от F8.

## Контекст

Attic рассматривается как Nix binary cache. Его потеря допустима, а trust к nar-файлам должен
определяться подписью и конфигурацией клиента, не расположением worker.

## Что сделать

- [x] Проверить Attic на текущем NixOS и закрепить выбранную версию/схему deployment.
      Выбран `attic-server` (даемон `atticd`) из закреплённого nixpkgs
      (0-unstable-2025-09-24), клиент — `attic-client` (CLI `attic`). Форка и
      локального пакета нет; linux-only, eval проходит на macOS, сборку ведёт CI.
      Открытый вопрос закрыт выбором `attic-server` из nixpkgs.
- [x] Настроить cache, signing keys через agenix и клиентский substituter.
      Модуль `lattice.attic` (`modules/attic/`): signing keypair генерируется и
      хранится server-side в БД; JWT admin-secret — agenix secret через
      `LoadCredential` (условный `pathExists`-паттерн, eval без `.age`).
      Клиент: `nix.settings.substituters` + `trusted-public-keys` только при
      заданных `trustedPublicKey`+`publicUrl` (иначе no-op placeholder).
- [ ] Собрать derivation, загрузить nar и восстановить его из чистого локального store path.
      Покрыто VM-тестом `tests/attic-vm.nix` (CI, x86_64-linux); исполняемое
      подтверждение — в CI. Подтверждение подписи/trust — в том же VM-тесте.
- [x] Описать очистку, лимиты и ротацию signing credentials.
      `modules/attic/README.md`: GC (`garbage-collection.interval`), retention,
      лимиты, ротация JWT-secret и signing keypair по [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md).

## Критерий готовности

- [ ] Подписанный nar принимается доверенным клиентом и отвергается без нужного trust config.
      Покрыто `tests/attic-vm.nix`; исполняемое подтверждение — в CI (x86_64-linux).
- [ ] Потеря Attic data вызывает rebuild/refetch, но не ломает воспроизводимость.
      Покрыто `tests/attic-vm.nix`; исполняемое подтверждение — в CI (x86_64-linux).

## Затрагиваемые файлы / слои

- `modules/attic/` — NixOS-модуль `lattice.attic`.
- `profiles/cache-plane/` — сервис в cache-plane профиле; порт `attic` в `ports.nix`.
- `nodes/mytecor-homelab/config.nix` — включение сервиса и agenix-обвязка JWT-secret.
- `nodes/mytecor-homelab/secrets/secrets.nix` — рецепт `attic-jwt-secret.age`.
- `tests/attic.nix`, `tests/attic-vm.nix` — eval-контракт и VM-тест trust/disposable.

## Открытые вопросы

Окончательный выбор Attic (attic-server из nixpkgs) сделан по результатам
оценки pinned-версии. Остаётся операторский шаг перед deploy: сгенерировать
signing keypair / создать кеш и JWT-secret, зашифровать `attic-jwt-secret.age`
в `[ admin node ]` (см. «Операторский шаг перед deploy» в `modules/attic/README.md`).

## Заметка по статусу (2026-09-13)

Код, модуль и eval завершены; `nix flake check --all-systems --no-build` проходит.
VM-тесты (`tests/attic.nix`, `tests/attic-vm.nix`) исполняются в CI на x86_64-linux;
до зелёного пропуска CI и создания `.age` пункты «Критерий готовности» остаются незакрытыми.
