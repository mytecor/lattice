# Antigravity через ACP без локального окружения

[Пакет](./package.nix) запускает официальный Agent Client Protocol (ACP) сервер Google Antigravity
(`agy-acp-server 1.4.0`), зарегистрированный в официальном ACP Registry (`antigravity-acp`).

Пакет предоставляет:
1. `antigravity-acp` — upstream ACP-сервер (`agy_acp_server.par` + `localharness_external`).
2. `antigravity-acp-fsless` — изолированный ACP-враппер для шлюза [`packages/agentrun-openai`](./../agentrun-openai/README.md), отключающий встроенные инструменты модели по аналогии с [`packages/codex-acp`](./../codex-acp/README.md).

## Механизм отключения встроенных инструментов

В отличие от Codex, у которого локальные инструменты отключаются через статический файл `environments.toml` (`default = "none"`), в `agy-acp-server` управление встроенными инструментами реализовано на уровне wire-протокола ACP через модуль `tool_filter.py`.

### Разделение категорий инструментов в Antigravity ACP

В `agy-acp-server` присутствуют три независимые категории инструментов:

1. **Клиентские инструменты файловой системы (`client_*`)**:
   `client_view_file`, `client_create_file`, `client_edit_file`.
   Подключаются **только** в том случае, если клиент в запросе `initialize` явно анонсирует файловые возможности (`clientCapabilities.fs.read_text_file` / `write_text_file`). Шлюз `agentrun-openai` (библиотека `agentrun`) передаёт пустые клиентские возможности (`{}`), поэтому эти инструменты к сессии никогда не подключаются.

2. **Встроенные инструменты харнесса (`BuiltinTools`)**:
   `run_command`, `view_file` (подменяемый на `make_safe_view_file`), `create_file`, `edit_file`, `list_directory`, `search_directory`, `find_file`, `ask_question`, `invoke_subagent`, `run_workflow`, `generate_image`, `search_web`, `read_url_content`, `schedule`, `finish`.
   Фильтрация этих инструментов управляется блоком `_meta.antigravity` в методах `session/new` и `session/load`:
   - `enabledTools`: allowlist встроенных инструментов. Любой инструмент вне этого списка выключается (`resolve_disabled_builtins()`).
   - `disabledTools`: denylist встроенных инструментов.
   - **Пустой allowlist (`"enabledTools": []`) отключает ВСЕ встроенные инструменты** без исключения. При этом `make_safe_view_file` не регистрируется, так как `BuiltinTools.VIEW_FILE` отключён.

3. **Клиентские MCP-инструменты (`mcpServers`)**:
   Инструменты, передаваемые клиентом в поле `mcpServers` запроса `session/new` (именно так `agentrun-openai` подкладывает свои сгенерированные MCP-инструменты из запроса OpenAI API).
   Фильтр `tool_filter` **не затрагивает** MCP-инструменты: они регистрируются в сессии штатно и остаются единственными доступными инструментами для модели.

### Работа враппера `antigravity-acp-fsless`

Поскольку `agentrun-openai` формирует стандартный ACP-запрос `session/new` без Antigravity-специфичного блока `_meta`, враппер `antigravity-acp-fsless`:
1. Требует и подготавливает выделенный каталог `$GEMINI_HOME` (аналог `CODEX_HOME` для Codex).
2. Создаёт минимальный `settings.json` с типом авторизации (`gemini-api-key`, `agent-platform` или `gateway`).
3. Запускает потоковый фильтр [`filter.mjs`](./filter.mjs) поверх `antigravity-acp`.
4. Фильтр перехватывает входящие вызовы `session/new` и `session/load` и прозрачно внедряет:
   ```json
   "_meta": {
     "antigravity": {
       "enabledTools": []
     }
   }
   ```
5. В результате Antigravity стартует сессию с пустым набором встроенных инструментов, и модель видит исключительно MCP-инструменты, переданные через `mcpServers`.

## Авторизация

Antigravity ACP сервер поддерживает следующие типы авторизации (`auth.type` в `$GEMINI_HOME/antigravity-acp/settings.json`):
- `gemini-api-key`: требует переменную окружения `GEMINI_API_KEY`.
- `agent-platform`: Google Cloud / Vertex AI (переменные `GOOGLE_API_KEY` или ADC `GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_LOCATION`).
- `gateway`: корпоративный или локальный AI-шлюз (переменная `AGY_LLM_GATEWAY_URL`, например для связи с [`packages/llm-gateway`](./../llm-gateway/README.md)).
- `oauth-personal` / `oauth-business`: интерактивная OAuth-авторизация.
