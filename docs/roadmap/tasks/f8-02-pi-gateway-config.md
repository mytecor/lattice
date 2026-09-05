# Подключить Pi к логическим моделям gateway

Фича: [F8 — интерактивный Pi runtime](../features/f8-pi-runtime.md). Зависит от
[f8-01](./f8-01-package-pi.md) и F7.

## Контекст

Pi не должен знать provider endpoints, credentials или реальные model IDs. Ему доступны только
gateway URL, client credential и четыре логических класса.

## Что сделать

- [ ] Сгенерировать декларативную Pi-конфигурацию с OpenAI-compatible gateway endpoint.
- [ ] Выдать Pi отдельный client credential через agenix/runtime boundary.
- [ ] Отключить provider-specific auto-discovery и перечислить только логические классы.
- [ ] Проверить streaming и переключение класса модели в TUI.

## Критерий готовности

- [ ] В Pi нет provider-specific конфигурации или upstream credentials.
- [ ] Все четыре logical models работают через gateway в streaming-режиме.

## Затрагиваемые файлы / слои

- `profiles/pi/`
- `nodes/mytecor-homelab/`
- `KEY_MANAGEMENT.md`

## Открытые вопросы

_нет_.
