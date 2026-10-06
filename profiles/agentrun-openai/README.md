## agentrun-openai

Профиль подключает `lattice.agentrun-openai` на ноде: loopback-листенер и
зарегистрированный порт. Runtime — пакет
[`packages/agentrun-openai`](../../packages/agentrun-openai/README.md), модуль —
[`modules/agentrun-openai`](../../modules/agentrun-openai/README.md).

Профиль не задаёт agent CLI (`path`, `backends`) — их подключает нода. На
`mytecor-homelab` включён бэкенд `pi = { command = "pi-acp"; }` (root-режим,
общий `/root/.pi/agent`), см. [`nodes/mytecor-homelab`](../../nodes/mytecor-homelab).

### Аутентификация agent CLI

agentrun-openai сам хранит native session-credentials agent CLI в `HOME`
пользователя `lattice.agentrun-openai.user`. На ноде гейтвей работает как root
(`user = "root"`, как `pi-acp-daemon`), поэтому спавняемый `pi-acp`
переиспользует общий root-конфиг Pi (`/root/.pi/agent`) и читает agenix-секрет
llm-gateway (`/run/agenix/llm-gateway-client-node-pi`) — отдельная
авторизация не нужна. Для не-root-запуска каждый агентский CLI должен быть
авторизован от имени системного юзера — operator-шаг на ноде.
