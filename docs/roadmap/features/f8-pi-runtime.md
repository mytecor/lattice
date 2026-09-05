# F8. Интерактивный Pi runtime

Pi становится основным harness разработки непосредственно на NixOS-ноде. Его runtime,
конфигурация моделей и набор tools сразу проектируются одинаковыми для интерактивного TUI и
будущего RPC-режима workers.

Зависит от [F7](./f7-llm-gateway.md). Соответствует
[вехе 8](../VISION.md#вехи-и-зависимости-без-деталей).

Задачи: [f8-01](../tasks/f8-01-package-pi.md),
[f8-02](../tasks/f8-02-pi-gateway-config.md),
[f8-03](../tasks/f8-03-reproducible-tool-profile.md),
[f8-04](../tasks/f8-04-interactive-acceptance.md),
[f8-05](../tasks/f8-05-pi-rpc-contract.md).

**Критерий готовности:** после декларативного rebuild пользователь запускает Pi TUI, выбирает
только логический класс модели и выполняет реальную задачу с `bash/git/tools`; provider-specific
настройки в Pi отсутствуют, а тот же runtime имеет проверенный RPC entry point для F10.

**Осознанно откладываем (до F10):** unattended execution, sandbox и жизненный цикл worker.
