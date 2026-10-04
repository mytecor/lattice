## agentrun-openai

Профиль подключает `lattice.agentrun-openai` на ноде: loopback-листенер и
зарегистрированный порт. Runtime — пакет
[`packages/agentrun-openai`](../../packages/agentrun-openai/README.md), модуль —
[`modules/agentrun-openai`](../../modules/agentrun-openai/README.md).

Профиль не задаёт agent CLI (`path`, `backends`) — их подключает нода, когда
готова предоставить authenticированные агентские бинари. Пока их нет, шлюз
отвечает на `/healthz` и `/v1/models`, а обращения к моделям падают с ошибкой
отсутствующего бинари — это ожидаемое промежуточное состояние.

### Аутентификация agent CLI

agentrun-openai сам хранит native session-credentials agent CLI в `HOME`
пользователя `lattice.agentrun-openai.user`. Для реального использования каждый
CLI должен быть авторизован от имени этого системного пользователя —
это operator-шаг на ноде (см. README модуля и roadmap).
