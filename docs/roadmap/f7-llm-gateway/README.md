# F7. LLM gateway

Pi и будущие workers обращаются к единой OpenAI-compatible точке входа, не знают provider
credentials и не зависят от конкретных providers или model IDs. Постоянный runtime — собственный
Go proxy поверх Bifrost Go API; Lattice владеет HTTP/API contract, routing и discovery policy, не
форкая Bifrost. Legacy `mxyhi/token_proxy` удалён из активной конфигурации после cutover
([f7-08](f7-08-remove-token-proxy.md)); его ограничения и отклонения Go LIP зафиксированы
в исторических f7-01, f7-05 и f7-06.

**Статус: выполнена 2026-09-07.**

Зависит от [F1](../f1-one-node/README.md) и [F2](../f2-secrets-identity/README.md). Соответствует
[вехе 7](../VISION.md#вехи-и-зависимости-без-деталей).

Все задачи закрыты: f7-01..f7-08. Матрица обязательных режимов F7 (retry, cooldown, fallback,
priority, race, hedge, streaming, cancellation), health surface и structured diagnostics
подтверждены Go-тестами и evaluation checks в
[f7-04](f7-04-routing-resilience-tests.md).

**Критерий готовности:** клиент с gateway credential выполняет streaming-запросы к логическим
моделям `stupid`, `standard`; реальные provider credentials и model IDs ему
недоступны; исчезновение primary native model допускает fallback только внутри назначенной access
group; retry, cooldown, fallback, priority, race и hedging проверены на управляемых сбоях.

**Осознанно откладываем (до F8):** конфигурацию Pi. Worker-specific выдачу credentials и сетевые
ограничения — до F10.
