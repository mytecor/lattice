# F8. Интерактивный Pi runtime

Pi — основной harness разработки на NixOS-ноде. Его runtime, конфигурация моделей и набор
tools едины для интерактивной работы и будущего stateless-режима workers. Локальный Pi TUI по
решению не используется: интерактивный ingress идёт через ACP, а будущая F10 execution boundary
запускает тот же Pi RPC runtime внутри контейнера, не вводя второй клиентский endpoint.

Интерактивная ветка публикует Pi как постоянный сетевой ACP daemon: один endpoint обслуживает
динамические параллельные сессии и позволяет нескольким клиентам подключаться к одной live session.
Эта ветка владеет ingress/session plane, но не меняет disposable-модель и контейнерную execution
boundary будущих workers.

Зависит от [F7](../f7-llm-gateway/README.md). Соответствует
[вехе 8](../../ROADMAP.md#f8-интерактивный-pi).

Задачи: [f8-01](./f8-01-package-pi.md),
[f8-02](./f8-02-pi-gateway-config.md),
[f8-03](./f8-03-reproducible-tool-profile.md),
[f8-04](./f8-04-interactive-acceptance.md),
[f8-05](./f8-05-pi-rpc-contract.md),
[f8-06](./f8-06-network-acp-daemon.md).

**Статус:** все задачи F8 закрыты ([f8-01](./f8-01-package-pi.md)…
[f8-06](./f8-06-network-acp-daemon.md)). [f8-04](./f8-04-interactive-acceptance.md) и
[f8-05](./f8-05-pi-rpc-contract.md) закрыты по решению «Pi TUI не используется, работа идёт через
ACP»: интерактивная acceptance выполнена через ACP, отдельного сетевого Pi RPC-контракта нет. ACP
endpoint остаётся ingress/session plane, а execution boundary для F10 задаёт контейнерный Pi runtime
из [f10-04](../f10-disposable-worker/f10-04-pi-rpc-runner.md).

**Критерий готовности:** после декларативного rebuild пользователь работает с логическим классом
модели и выполняет реальную задачу с `bash/git/tools` через ACP-клиента (Ferngeist) — Pi TUI не
используется, интерактивная работа ведётся через ACP; provider-specific настройки в Pi
отсутствуют; сетевые ACP-клиенты через один endpoint создают параллельные сессии и подключаются к
общей live session. Тот же runtime имеет проверенный RPC mode, но F10 запускает его за контейнерной
execution boundary; закреплённый ACP endpoint из [f8-06](./f8-06-network-acp-daemon.md) остаётся
клиентской точкой входа, отдельный сетевой Pi RPC endpoint не вводится.

**Осознанно откладываем (до F10):** unattended execution, sandbox и жизненный цикл worker.
