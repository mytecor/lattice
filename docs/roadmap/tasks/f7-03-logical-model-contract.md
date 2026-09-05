# Зафиксировать контракт логических моделей

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md). Зависит от
[f7-02](./f7-02-declarative-gateway-service.md).

## Контекст

Pi, workers, controller и task definitions должны знать только `cheap`, `standard`, `strong` и
`frontier`. Замена реальных моделей и providers — внутренняя операция gateway.

## Что сделать

- [ ] Описать семантику четырёх классов и допустимые изменения их mappings.
- [ ] Настроить mappings на несколько upstream без provider-specific IDs на client boundary.
- [ ] Ограничить или заменить model discovery так, чтобы он не раскрывал реальные модели.
- [ ] Добавить контрактные проверки запросов, ошибок и списка доступных моделей.

## Критерий готовности

- [ ] Клиент успешно использует все четыре логических имени и не получает реальные model IDs.
- [ ] Смена provider/model mapping не требует изменения клиента или task specification.

## Затрагиваемые файлы / слои

- `profiles/llm-gateway/`
- `checks/`
- `ARCHITECTURE.md`

## Открытые вопросы

_нет_. Конкретные mappings являются конфигурацией эксплуатации, а не частью контракта.
