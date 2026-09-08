# Развернуть и проверить Attic

Фича: [F9 — cache и artifact plane](./README.md). Зависит от F8.

## Контекст

Attic рассматривается как Nix binary cache. Его потеря допустима, а trust к nar-файлам должен
определяться подписью и конфигурацией клиента, не расположением worker.

## Что сделать

- [ ] Проверить Attic на текущем NixOS и закрепить выбранную версию/схему deployment.
- [ ] Настроить cache, signing keys через agenix и клиентский substituter.
- [ ] Собрать derivation, загрузить nar и восстановить его из чистого локального store path.
- [ ] Описать очистку, лимиты и ротацию signing credentials.

## Критерий готовности

- [ ] Подписанный nar принимается доверенным клиентом и отвергается без нужного trust config.
- [ ] Потеря Attic data вызывает rebuild/refetch, но не ломает воспроизводимость.

## Затрагиваемые файлы / слои

- `modules/attic/`
- `profiles/cache-plane/`
- `nodes/mytecor-homelab/secrets/`

## Открытые вопросы

Окончательный выбор Attic закрывается после executable проверки.
