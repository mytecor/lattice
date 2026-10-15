# Gemini CLI через ACP без локального окружения

[Пакет](./package.nix) запускает официальный **Google Gemini CLI** в режиме Agent Client Protocol (ACP) по флагу `--acp`.

Пакет предоставляет:
1. `gemini-acp` — upstream ACP-сервер (`gemini --acp`).
2. `gemini-acp-fsless` — изолированный ACP-враппер для шлюза [`packages/agentrun-openai`](./../agentrun-openai/README.md), отключающий встроенные инструменты харнесса (локальную файловую систему, выполнение bash-команд, встроенные утилиты) по аналогии с [`packages/codex-acp`](./../codex-acp/README.md) и [`packages/antigravity-acp`](./../antigravity-acp/README.md).

## Механизм изоляции и отключения встроенных инструментов

Чтобы предотвратить попытки агента читать и писать в локальную файловую систему или выполнять bash-команды на хосте в обход MCP-прослойки от `agentrun-openai`, враппер реализует многоуровневую изоляцию:

### 1. Политика Policy Engine (`fsless.toml`)

Gemini CLI включает Policy Engine с поддержкой многоуровневых политик (Tiers). Административные политики (Admin Tier, Base 5) обладают наивысшим приоритетом и перекрывают любые пользовательские настройки.

Враппер передаёт файл политик [`fsless.toml`](./fsless.toml) через аргумент `--admin-policy` и симлинкует его в `$GEMINI_CLI_HOME/.gemini/policies/fsless.toml`.

Политика:
- Запрещает (`decision = "deny"` с приоритетом 999) все встроенные инструменты:
  - Выполнение команд и процессов: `run_shell_command`, `list_background_processes`, `read_background_output`.
  - Файловые операции: `read_file`, `read_many_files`, `write_file`, `replace`, `list_directory`, `glob`, `grep_search`, `search_file_content`.
  - Веб-инструменты: `google_web_search`, `web_fetch`.
  - Внутренние утилиты и под-агенты: `activate_skill`, `get_internal_docs`, `enter_plan_mode`, `exit_plan_mode`, `tracker_*`, `update_topic`, `write_todos`, `ask_user`, `invoke_agent`.
- В соответствии с контрактом Policy Engine, глобально запрещённые инструменты (`decision = "deny"` без `argsPattern`) полностью исключаются из контекста модели (`getExcludeTools`), предотвращая лишние запросы.
- Разрешает (`decision = "allow"` с приоритетом 999) инструменты от внешних MCP-серверов (`toolName = "*", mcpName = "*"`).

### 2. Конфигурация `settings.json`

Враппер подготавливает `$GEMINI_CLI_HOME/.gemini/settings.json`, в котором поле `tools.exclude` дублирует список исключаемых встроенных инструментов, гарантируя их удаление из реестра активных инструментов на этапе инициализации конфигурации CLI.

### 3. Протокольный фильтр `filter.mjs`

Потоковый фильтр [`filter.mjs`](./filter.mjs) перехватывает входящие запросы JSON-RPC на `stdin`:
- В запросе `initialize` удаляет возможности `clientCapabilities.fs` и `clientCapabilities.terminal` (если они были переданы клиентом), гарантируя, что Gemini CLI не создаст проксирующие файловые сервисы `AcpFileSystemService`.

### 4. Изоляция рабочего окружения

- Враппер требует выделенный каталог `$GEMINI_HOME` (задаёт права `0700`) и экспортирует `GEMINI_CLI_HOME="$GEMINI_HOME"`.
- Устанавливает `GEMINI_CLI_TRUST_WORKSPACE="true"`, отключая интерактивные проверки доверия директориям в автономном режиме.

## Авторизация

Поддерживаются следующие типы авторизации (`auth.selectedType` в `$GEMINI_CLI_HOME/.gemini/settings.json`):
- `gemini-api-key`: требует переменную окружения `GEMINI_API_KEY`.
- `vertex-ai`: требует `GOOGLE_API_KEY` или ADC `GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_LOCATION`.
- `gateway`: корпоративный или локальный AI-шлюз (переменная `GOOGLE_GEMINI_BASE_URL`).

## Связанные компоненты

- CLI-пакет: [`packages/gemini-cli`](./../gemini-cli/README.md)
- OpenAI-совместимый шлюз: [`packages/agentrun-openai`](./../agentrun-openai/README.md)
