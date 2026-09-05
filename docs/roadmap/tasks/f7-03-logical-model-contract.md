# Зафиксировать контракт логических моделей

Фича: [F7 — LLM gateway](../features/f7-llm-gateway.md). Зависит от
[f7-02](./f7-02-declarative-gateway-service.md).

## Контекст

Pi, workers, controller и task definitions должны знать только `cheap`, `standard`, `strong` и
`frontier`. Замена реальных моделей и providers — внутренняя операция gateway.

## Что сделать

- [x] Описать семантику четырёх классов и допустимые изменения их mappings.
- [x] Настроить mappings на несколько upstream без provider-specific IDs на client boundary.
- [x] Ограничить или заменить model discovery так, чтобы он не раскрывал реальные модели.
- [x] Добавить контрактные проверки запросов, ошибок и списка доступных моделей.

## Критерий готовности

- [x] Клиент успешно использует все четыре логических имени и не получает реальные model IDs.
- [x] Смена provider/model mapping не требует изменения клиента или task specification.

## Результат

Выполнено 2026-09-05 на executable gateway spike. Все четыре client ID проходят через разные
управляемые mappings; request и response rewrite сохраняют логическое имя. `/v1/models` возвращает
ровно контрактный набор, а попытка вызвать provider-specific ID получает 404 без обращения к
upstream.

Профиль фиксирует контракт в `lattice.llm-gateway.logicalModels`. NixOS-модуль отклоняет
конфигурации с prefixed discovery, отсутствующим классом, лишним advertised ID или logical model
без mapping. Семантика классов и правила безопасной замены mapping зафиксированы в
[`ARCHITECTURE.md`](../../../ARCHITECTURE.md#контракт-логических-моделей).

## Затрагиваемые файлы / слои

- `profiles/llm-gateway/`
- `checks/`
- `ARCHITECTURE.md`

## Открытые вопросы

_нет_. Конкретные mappings являются конфигурацией эксплуатации, а не частью контракта.
