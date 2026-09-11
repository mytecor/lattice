# F7. LLM gateway

Pi и будущие workers обращаются к единой OpenAI-compatible точке входа, не знают provider
credentials и не зависят от конкретных providers или model IDs. Постоянный runtime — собственный
Go proxy поверх Bifrost Go API; Lattice владеет HTTP/API contract, routing и discovery policy, не
форкая Bifrost. Legacy `mxyhi/token_proxy` удалён из активной конфигурации после cutover
([f7-08](f7-08-remove-token-proxy.md)); его ограничения и отклонения Go LIP зафиксированы
в исторических f7-01, f7-05 и f7-06.

**Статус:** базовая реализация выполнена 2026-09-07; после live acceptance routing открыты
[f7-09](./f7-09-bounded-provider-routing.md),
[f7-10](./f7-10-route-native-provider-mapping.md),
[f7-11](./f7-11-typed-routing-rules.md),
[f7-12](./f7-12-flat-routing-named-routes-filter.md) и
[f7-13](./f7-13-provider-balancing.md).

Зависит от [F1](../f1-one-node/README.md) и [F2](../f2-secrets-identity/README.md). Соответствует
[вехе 7](../VISION.md#вехи-и-зависимости-без-деталей).

Базовые задачи f7-01..f7-08 закрыты. Follow-up
[f7-09](./f7-09-bounded-provider-routing.md) декомпозирует flat routing pipeline и ограничивает
upstream fanout, выявленный live Pi-сессией;
[f7-10](./f7-10-route-native-provider-mapping.md) переносит native mapping и provider selection
в routing rules; [f7-11](./f7-11-typed-routing-rules.md) разделяет внешний rule contract и
реализации по action types; [f7-12](./f7-12-flat-routing-named-routes-filter.md) переводит
routing на named routes + `filter` + явные `target`-переходы (retry/fallback как именованные
subroutes, без primary/fallback stage); [f7-13](./f7-13-provider-balancing.md) фиксирует дизайн
round-robin и адаптивной балансировки провайдеров по здоровью (live-наблюдение: при большом RPC
весь трафик валится на самого быстрого, `hyperfusion` при `priority: 100` не выигрывает ни одного
race) — реализация отложена. Матрица обязательных режимов F7 (retry, cooldown,
fallback, priority, race, hedge, streaming, cancellation), health surface и structured
diagnostics подтверждены Go-тестами и evaluation checks в [f7-04](./f7-04-routing-resilience-tests.md).

**Критерий готовности:** клиент с gateway credential выполняет streaming-запросы к логическим
моделям `stupid`, `standard`; реальные provider credentials и model IDs ему
недоступны; исчезновение primary native model допускает fallback только внутри назначенной access
group; retry, cooldown, fallback, priority, race и hedging проверены на управляемых сбоях.

**Осознанно откладываем (до F8):** конфигурацию Pi. Worker-specific выдачу credentials и сетевые
ограничения — до F10.
