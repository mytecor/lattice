# Собрать декларативный LLM gateway service

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md). Зависит от
[f7-01](./f7-01-token-proxy-spike.md).

## Контекст

Routing, upstream metadata, priorities, aliases и retry/race/hedging policies являются открытой
декларативной конфигурацией. Через agenix поступают только API/OAuth/gateway credentials; весь
gateway config одним secret-файлом не хранится.

## Что сделать

- [ ] Добавить NixOS-модуль и профиль gateway с typed options для несекретной конфигурации.
- [ ] Подать credentials из отдельных agenix secrets, не копируя их в Nix store.
- [ ] Запускать сервис под отдельным пользователем с минимальными filesystem/network правами.
- [ ] Описать bootstrap, ротацию gateway/client и provider credentials.

## Критерий готовности

- [ ] `nixos-rebuild` разворачивает работающий gateway без ручного редактирования файлов.
- [ ] В Git, Nix store и публичной конфигурации нет secret values; ротация не требует менять Pi.

## Затрагиваемые файлы / слои

- `modules/llm-gateway/`
- `profiles/llm-gateway/`
- `nodes/mytecor-homelab/`
- `KEY_MANAGEMENT.md`

## Открытые вопросы

Точный механизм безопасной сборки runtime config определяется по результату f7-01.
