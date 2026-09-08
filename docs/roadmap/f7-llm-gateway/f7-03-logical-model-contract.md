# Зафиксировать контракт логических моделей

Фича: [F7 — LLM gateway](./README.md). Зависит от
[f7-02](./f7-02-declarative-gateway-service.md).

## Контекст

Pi, workers, controller и task definitions временно знают только `stupid` и `standard`. Замена
реальных моделей и providers остаётся внутренней операцией gateway.

## Что сделать

- [x] Описать семантику публичных классов и допустимые изменения их mappings.
- [x] Настроить mappings на несколько upstream без provider-specific IDs на client boundary.
- [x] Ограничить или заменить model discovery так, чтобы он не раскрывал реальные модели.
- [x] Добавить контрактные проверки запросов, ошибок и списка доступных моделей.

## Критерий готовности

- [x] Клиент успешно использует оба логических имени и не получает реальные model IDs.
- [x] Смена provider/model mapping не требует изменения клиента или task specification.

## Результат

Первоначальный четырёхклассовый контракт проверен 2026-09-05 на executable gateway spike. Решением
от 2026-09-07 активный набор сокращён до `stupid → MiniMaxAI/MiniMax-M2.7` и
`standard → deepseek-ai/DeepSeek-V4-Flash-0731`; обе модели используют access group `gonka` с
parallel race Proxy/OpenBroker. Request/response rewrite сохраняет logical имя, а попытка вызвать
provider-specific ID получает 404 до обращения к upstream.

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
