# F7. LLM gateway

Pi и будущие workers обращаются к единой OpenAI-compatible точке входа, не знают provider
credentials и не зависят от конкретных providers или model IDs. Основной кандидат —
`mxyhi/token_proxy`; выбор подтверждается executable spike, а не только документацией.

Зависит от [F1](./f1-one-node.md) и [F2](./f2-secrets-identity.md). Соответствует
[вехе 7](../VISION.md#вехи-и-зависимости-без-деталей).

Задачи: [f7-01](../tasks/f7-01-token-proxy-spike.md),
[f7-02](../tasks/f7-02-declarative-gateway-service.md),
[f7-03](../tasks/f7-03-logical-model-contract.md),
[f7-04](../tasks/f7-04-routing-resilience-tests.md).

**Критерий готовности:** клиент с gateway credential выполняет streaming-запросы к логическим
моделям `cheap`, `standard`, `strong`, `frontier`; реальные provider credentials и model IDs ему
недоступны; retry, cooldown, fallback, priority, race и hedging проверены на управляемых сбоях.

**Осознанно откладываем (до F8):** конфигурацию Pi. Worker-specific выдачу credentials и сетевые
ограничения — до F10.
