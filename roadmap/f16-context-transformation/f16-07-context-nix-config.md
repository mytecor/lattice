# f16-07. Декларативная конфигурация и безопасное включение

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-01](./f16-01-context-contract-budget.md); завершение после [f16-06](./f16-06-context-routing-integration.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Добавить typed contextTransformation policy по logical model: enable, thresholds, tail/segment limits, capabilities/tokenizer, summarizer и cache settings; дефолт выключен.
- [ ] Сериализовать policy в runtime JSON отдельно от flat routing actions; поддержать models sugar и ручные routes без скрытого изменения balancing/access.
- [ ] Универсальные ограничения выразить module assertions: target < soft < hard, валидные quotas/limits, известные targets и полный capability coverage. Продублировать необходимую validation для standalone Go JSON.
- [ ] Определить service-owned cache directory, opt-in persistence/ephemeral-root wiring и cleanup; summaries не попадают в Nix store/public config.
- [ ] Описать поэтапное включение на выбранной logical model homelab и rollback одним disable; не включать автоматически для всех клиентов до acceptance.

## Критерий готовности

- [ ] Изолированные Nix contract tests проверяют valid/invalid policies и семантику generated JSON, без snapshot конкретных node defaults.
- [ ] Go принимает generated config; disabled сохраняет существующие API/routing contracts, service имеет доступ только к предусмотренному cache directory.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [modules/llm-gateway/options.nix](./../../modules/llm-gateway/options.nix)
- [modules/llm-gateway/types.nix](./../../modules/llm-gateway/types.nix)
- [modules/llm-gateway/config.nix](./../../modules/llm-gateway/config.nix)
- [modules/llm-gateway/README.md](./../../modules/llm-gateway/README.md)
- [profiles/llm-gateway/README.md](./../../profiles/llm-gateway/README.md)
- [packages/llm-gateway/config.go](./../../packages/llm-gateway/config.go)
- [tests/llm-gateway-bifrost.nix](./../../tests/llm-gateway-bifrost.nix)
- [tests/llm-gateway-sugar.nix](./../../tests/llm-gateway-sugar.nix)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
