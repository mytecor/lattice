# Собрать декларативный LLM gateway service

Фича: [F7 — LLM gateway](./README.md). Зависит от
[f7-01](./f7-01-token-proxy-spike.md).

## Контекст

Routing, upstream metadata, priorities, aliases и retry/race/hedging policies являются открытой
декларативной конфигурацией. Через agenix поступают только API/OAuth/gateway credentials; весь
gateway config одним secret-файлом не хранится.

## Что сделать

- [x] Добавить NixOS-модуль и профиль gateway с typed options для несекретной конфигурации.
- [x] Подать credentials из отдельных agenix secrets, не копируя их в Nix store.
- [x] Запускать сервис под отдельным пользователем с минимальными filesystem/network правами.
- [x] Описать bootstrap, ротацию gateway/client и provider credentials.

## Критерий готовности

- [ ] `nixos-rebuild` разворачивает работающий gateway без ручного редактирования файлов.
- [ ] В Git, Nix store и публичной конфигурации нет secret values; ротация не требует менять Pi.

## Затрагиваемые файлы / слои

- `modules/llm-gateway/`
- `profiles/llm-gateway/`
- `nodes/mytecor-homelab/`
- `KEY_MANAGEMENT.md`

## Открытые вопросы

Безопасная сборка runtime config определена: secret-free JSON создаётся в Nix store, а
`ExecStartPre` подставляет отдельные systemd credentials в приватный файл mode `0600`.

Account-backed OAuth остаётся ограничением закреплённого headless CLI: декларативной команды
импорта нет, identity хранится в SQLite. Модуль не выдаёт API key за OAuth record и пока принимает
только API-key upstreams.

## Статус

Модуль, профиль, evaluation-check и NixOS VM-тест добавлены 2026-09-05. Полная evaluation проходит,
но Linux VM-тест и фактический `nixos-rebuild` homelab ещё не выполнены: для ноды нужны реальные
зашифрованные client/provider secrets и выбранные operational mappings. Поэтому критерии готовности
выше остаются открытыми, а f7-02 — в работе.
