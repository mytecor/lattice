# Muse ACP для agentrun-openai

[Пакет](./package.nix) закрепляет community-адаптер
[BrokkAi/muse-acp](https://github.com/BrokkAi/muse-acp) `0.15.0`
для `x86_64-linux` и `aarch64-linux`. Он переводит ACP stdio в MSP
процесса `muse serve`; Linux ELF адаптируется через `autoPatchelfHook`.
Официальный [Muse Code](./../muse/README.md) закреплён отдельно.

`pkgs.lattice.muse-acp` предоставляет обычный адаптер.
Шлюз использует `pkgs.lattice.muse-acp-fsless` из
[wrapper](./fsless.nix) и [ACP supervisor](./fsless.mjs).

## Инструменты и окружение

Wrapper требует отдельный `MUSE_HOME` и помещает туда `HOME` и все четыре
XDG-каталога. Конфигурация и авторизация других агентов не используются.
Workspace не объявляется доверенным, поэтому его инструкции, skills и hooks
не подключаются к контексту модели.

Muse запускается с `--disable-write`, `--disable-shell` и
`--disable-request-user-input`. Встроенные инструменты исключаются через
`run.toolset`, а фоновые reminder agents отключаются через
`run.reminder_roster.agents=[]`. С клиентскими функциями разрешается только
`mcp__openai_functions__*` — нормализованное имя session-scoped MCP-моста
`openai-functions` из agentrun-openai. Без клиентских функций toolset пустой.
Внешние `MUSE_SERVE_ARGS` и `MUSE_CLI` не могут отменить эти настройки.

Muse отвергает MCP-селектор без соответствующего сервера. Поэтому supervisor
сначала обслуживает discovery с пустым toolset, а перед первой ACP
`session/new`, `session/load` или `session/resume` с MCP перезапускает адаптер
с нужным селектором и повторяет initialization. Сессия к этому моменту ещё
не создана. Один ACP-процесс обслуживает один набор MCP-инструментов;
смена наличия MCP требует нового процесса, как и в session lifecycle шлюза.
Сторонние MCP-серверы в этом wrapper не принимаются.

Это ограничение доступных модели инструментов. Muse сохраняет доступ
процесса к своей авторизации, session storage и запуску MCP transport.

## Homelab и авторизация

[Конфигурация ноды](./../../nodes/mytecor-homelab/muse.nix) добавляет backend
`muse = muse-acp-fsless` в [agentrun-openai](./../agentrun-openai/README.md).
Его состояние лежит в `/var/lib/agentrun-openai/muse` внутри сохраняемого
StateDirectory шлюза. После применения конфигурации оператор входит на ноде:

```sh
muse-agentrun login
```

`muse-agentrun` использует ту же закреплённую CLI и тот же `MUSE_HOME`, что и
шлюз. Для ручного запуска wrapper вне ноды:

```sh
MUSE_HOME=/path/to/private/muse-home muse-acp-fsless
```

До входа Meta-запросы потребуют авторизацию. Содержимое auth cache не выводится
в чат и не помещается в Git или Nix store. Публикация и применение конфигурации
описаны в [процедуре развёртывания](./../../DEPLOYMENT.md).

## Проверки

[Runtime-тест](./../../tests/muse-acp-smoke.mjs) запускает настоящие Muse и
ACP-адаптер против локального тестового провайдера с фиктивным ключом.
Он проверяет фактический список model-visible tools: только MCP-функция с
клиентскими инструментами и пустой список без них. Workspace-инструкция
с контрольной строкой не должна попасть в запрос к модели.
Тест включён в `checks.x86_64-linux.muse-acp` через
[Nix check](./../../tests/muse-acp.nix).
