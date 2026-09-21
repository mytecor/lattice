# f16-09. Контрактная и runtime-приёмка context MVP

**Статус:** не начата; план от 2026-09-21.

## Контекст

Часть [F16. Context transformation](./README.md).
Зависимости: [f16-06](./f16-06-context-routing-integration.md), [f16-07](./f16-07-context-nix-config.md), [f16-08](./f16-08-context-observability.md). Контракты и failure policy из design обязательны для этой задачи.

## Что сделать

- [ ] Собрать обезличенный corpus: длинная coding session, повторные reads, tests/errors, смены задач, возврат к старому решению, parallel tools, huge turn, unknown/multimodal blocks.
- [ ] Автоматически проверить protocol validity, exact tail/source invariants, hard budget, append/cache stability и отсутствие посторонних pages; semantic качество оценивать по сохранённым facts/anchors, не exact prose summary.
- [ ] Сравнить passthrough, tool-only и полный pipeline: final input tokens, cold/warm added latency, summary usage/cost, retrieval hit/miss и сохранность решений. В отчёте указать corpus, model/version и измерения; не обещать коэффициент сжатия без данных.
- [ ] Выполнить Go tests включая race detector для concurrent cache, Nix eval/generated-artifact checks по политике репозитория; проверить сборку закреплённого Go/Nix package.
- [ ] Проверить Pi через ACP на test logical model: gateway получает полную history, собственная compaction клиента не скрывает raw source. Затем opt-in homelab smoke, restart cache и rollback.
- [ ] Описать ограничения, unsupported formats и post-MVP backlog: hierarchy, embeddings/hybrid, detailed/raw promotion, prefetch, safe file diffs.

## Критерий готовности

- [ ] Все автоматические invariants проходят; below-threshold вызывает ноль summaries, warm append переиспользует sealed pages, target достигается на compressible corpus, hard budget никогда не превышен.
- [ ] Отчёт фиксирует качественные и стоимостные результаты, live routing/continue не регрессируют; feature можно отключить без восстановления cache или изменения клиента.

Проверки добавлять по [политике тестов](./../../tests/README.md): контракты и поведение,
без snapshots production-конфигурации.

## Затрагиваемые файлы / слои

- [packages/llm-gateway/README.md](./../../packages/llm-gateway/README.md)
- [packages/llm-gateway/package.nix](./../../packages/llm-gateway/package.nix)
- [tests/README.md](./../../tests/README.md)
- [tests/llm-gateway-bifrost.nix](./../../tests/llm-gateway-bifrost.nix)
- [tests/llm-gateway-sugar.nix](./../../tests/llm-gateway-sugar.nix)
- [nodes/mytecor-homelab/config.nix](./../../nodes/mytecor-homelab/config.nix)

Новые Go-компоненты и тесты размещать рядом с gateway; указанные точки интеграции
не требуют реализации всей задачи в одном файле.
