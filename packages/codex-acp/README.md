# Codex через ACP без локального окружения

[Пакет](./package.nix) запускает `@agentclientprotocol/codex-acp 2.1.1`
и Codex `0.161.0`, закреплённые в [lockfile](./pnpm-lock.yaml) и
[правилах зависимостей](./pnpm-workspace.yaml). Используется upstream без патчей.
На homelab [конфигурация](./../../nodes/mytecor-homelab/codex.nix)
добавляет backend `codex` в agentrun-openai после появления зашифрованной авторизации.

## Инструменты

При запуске wrapper устанавливает [environments.toml](./environments.toml)
в отдельный `CODEX_HOME`:

```toml
default = "none"
include_local = true
```

`default = "none"` не прикрепляет локальное окружение к сессии модели.
`include_local = true` оставляет runtime для stdio MCP: в Codex 0.161.0
`include_local = false` запрещает также запуск таких MCP-серверов.
Проверка подтверждает, что при этой конфигурации модель получает MCP-инструмент,
но не получает инструментов для работы с локальным окружением.

Codex не предоставляет модели инструменты
для выполнения команд и работы с локальными файлами. Дополнительно выключены
shell, web search, image generation, subagents, goals, apps и code mode.
Клиентские инструменты приходят через ACP `mcpServers` из agentrun-openai.
Старая переменная `CODEX_EXEC_SERVER_URL` больше не используется.

Адаптер 2.x запускает отдельный Codex App Server. Его session-настройки задаются
через `CODEX_CONFIG` в wrapper; прежние аргументы `-c` Rust-адаптера здесь
не используются. Версии обоих компонентов обновляются явно вместе с
[runtime-проверкой](./../../tests/codex-acp-smoke.mjs): тест проверяет фактический
список инструментов в запросе к локальному тестовому провайдеру, без реальных ключей.

Отключение окружения **не убирает все служебные инструменты Codex**:
`update_plan`, `request_user_input` и MCP resource helpers могут оставаться.
Это отсутствие локальных инструментов у модели, а не изоляция процесса от ОС:
Codex по-прежнему читает свой конфиг, обновляет авторизацию и запускает MCP transport.

## Авторизация ChatGPT

Нужен полный файл `auth.json`, созданный Codex CLI после входа через ChatGPT,
с access token и refresh token. Логин, пароль и browser cookie не нужны.
[Официальная документация](https://learn.chatgpt.com/docs/auth)
описывает file storage и перенос auth cache на headless-машину.

Вход выполняется непосредственно на ноде, в сохраняемый каталог
`/var/lib/agentrun-openai/codex`, с `cli_auth_credentials_store="file"`:

```sh
# На ноде: codex — закреплённая версия 0.161.0.
env CODEX_HOME=/var/lib/agentrun-openai/codex \
  codex -c 'cli_auth_credentials_store="file"' login --device-auth
```

Оператор открывает показанную ссылку и вводит одноразовый device code в браузере.
После подтверждения агент проверяет статус входа и передаёт auth cache по SSH
прямо в stdin `age`, без вывода содержимого и без открытой копии на Mac.
В Git коммитится только [зашифрованный auth cache](./../../nodes/mytecor-homelab/secrets/codex-auth.age),
зашифрованный для admin и node по
[правилам получателей](./../../nodes/mytecor-homelab/secrets/secrets.nix).
Не вставляйте содержимое auth cache в чат и не запускайте его вывод в терминал.

agenix расшифровывает секрет на ноде; systemd передаёт его через `LoadCredential`.
[Инициализатор](./../../scripts/seed-codex-auth.sh) кладёт рабочую копию с правами
`0600` в отдельный `CODEX_HOME` внутри сохраняемого StateDirectory agentrun-openai.
Обновлённые Codex токены переживают restart/reboot: неизменившийся seed не
перезаписывает рабочий файл. Замена зашифрованного секрета вызывает restart
сервиса и импорт нового login. Если persistent storage потерян, старый seed
после ротации refresh token может потребовать повторного входа и шифрования.

До появления секрета backend не регистрируется. После шифрования необходимы
публикация и применение конфигурации по [процедуре развёртывания](./../../DEPLOYMENT.md).
Проверка с настоящей ChatGPT-авторизацией выполняется после этого шага.
