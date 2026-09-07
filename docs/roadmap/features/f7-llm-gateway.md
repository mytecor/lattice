# F7. LLM gateway

Pi и будущие workers обращаются к единой OpenAI-compatible точке входа, не знают provider
credentials и не зависят от конкретных providers или model IDs. `mxyhi/token_proxy` остаётся
только временным runtime до cutover: его ограничения подтверждены реальной интеграцией, а Go LIP
отклонён в f7-06. Постоянный runtime — собственный Go proxy поверх Bifrost Go API; Lattice владеет
HTTP/API contract, routing и discovery policy, не форкая Bifrost.

Зависит от [F1](./f1-one-node.md) и [F2](./f2-secrets-identity.md). Соответствует
[вехе 7](../VISION.md#вехи-и-зависимости-без-деталей).

Задачи: [f7-01](../tasks/f7-01-token-proxy-spike.md),
[f7-02](../tasks/f7-02-declarative-gateway-service.md),
[f7-03](../tasks/f7-03-logical-model-contract.md),
[f7-04](../tasks/f7-04-routing-resilience-tests.md),
[f7-05](../tasks/f7-05-research-gateway-alternatives.md),
[f7-06](../tasks/f7-06-go-lip-gonka-cutover.md),
[f7-07](../tasks/f7-07-bifrost-go-proxy.md).

**Критерий готовности:** клиент с gateway credential выполняет streaming-запросы к логическим
моделям `stupid`, `standard`; реальные provider credentials и model IDs ему
недоступны; исчезновение primary native model допускает fallback только внутри назначенной access
group; retry, cooldown, fallback, priority, race и hedging проверены на управляемых сбоях.

**Осознанно откладываем (до F8):** конфигурацию Pi. Worker-specific выдачу credentials и сетевые
ограничения — до F10.
