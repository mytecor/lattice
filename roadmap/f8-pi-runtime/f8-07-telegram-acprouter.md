# Telegram-клиент ACP через `vcoderun/acprouter`

Фича: [F8 — интерактивный Pi runtime](./README.md), follow-up к закрытой
[f8-06](./f8-06-network-acp-daemon.md). Зависит от закреплённого LAN ACP endpoint
`ws://acp.<nodename>.local/` (f8-06) — acprouter подключается к нему как ещё один чистый
ACP WebSocket-клиент, как Ferngeist, и не требует изменений ingress.

## Контекст

F8 закрепил ACP endpoint для десктопных клиентов (Ferngeist), но с телефона узел недоступен:
мобильные ACP-клиенты отсутствуют. [`vcoderun/acprouter`](https://github.com/vcoderun/acprouter) —
готовый Python-клиент (MVP, alpha), который слушает Telegram и работает ACP-агентом поверх
stdlib-процесса: один активный ACP session на чат, `/stop`, проекция approvals (инлайн-кнопки),
plan/tool updates, стриминг ответов.

Ровно тот же ACP, что и у Ferngeist, но transport — stdio↔WebSocket. Два способа подключить
acprouter к сетевому endpoint (форма соединения Ferngeist — WebSocket + subprotocol `acp.v1`):

1. **Мост на ноде**: рядом с daemon поднимается локальный WebSocket→stdio мост
   (форма Ferngeist на одной стороне, stdio ACP на другой — смежный с отложенным Zed-shim
   класс задачи), acprouter запускается локально с `ACPROUTER_COMMAND` = этот мост.
2. **Stock bridge** — по README acprouter, remote ACP server зеркалируется в stdio через
   `acpkit`/`acpremote` (`acpkit run --addr ws://remote:8080/acp/ws`), если транспорт
   совместим с формой соединения Ferngeist; не подтверждено, требует проверки.

При обоих вариантах acprouter остаётся клиентской поверхностью: daemon (hydra-acp) владеет
сессиями и tools, acprouter только проецирует в Telegram и отвечает на approvals. Секреты
Telegram (api id/hash, bot token) идут через agenix по конвенциям проекта — в конфиг и логи
не попадают.

## Что сделать

- [ ] Закрепить `acprouter` в `packages/` (Python 3.11+, pyproject с `uv.lock`; собрать
      через `python3Packages.buildPythonApplication` или uv-based fetcher, hash зафиксировать),
      экспортировать как `pkgs.lattice.acprouter` из корневого flake.
- [ ] Проверить транспорт до Lattice endpoint: подтверждённая форма соединения Ferngeist —
      WebSocket `ws://acp.<nodename>.local/` с subprotocol `acp.v1` без token и path;
      выбрать способ моста (локальный мост на ноде или `acpkit`/`acpremote`-зеркало) и
      проверить его совместимость на закреплённой версии acprouter.
- [ ] Добавить NixOS-модуль `lattice.acprouter` в `modules/acprouter/`
      (`options.nix` / `config.nix` / `default.nix` + `README.md` по конвенциям проекта):
      systemd-сервис запускает acprouter с `ACPROUTER_COMMAND` = мост к
      `ws://acp.<nodename>.local/`; env `ACPROUTER_WORKSPACE_ROOT`, `ACPROUTER_STATE_DIR`
      (StateDirectory), `ACPROUTER_LOG_LEVEL`; Telegram-ключи — из `agenix` secret-файлов
      (`TELEGRAM_API_ID`, `TELEGRAM_API_HASH`, `TELEGRAM_BOT_TOKEN`), значение не появляется
      в конфиге и логах.
- [ ] Строгий песочник по образцу `pi-acp-daemon` (защита от выхода за workspace, `NoNewPrivileges`,
      `ProtectSystem`, минимальные capabilities); `ACPROUTER_ENABLE_HOST_TOOLS` — по умолчанию
      выключен (host tools против workspace-а ноды — сознательное решение владельца, а не дефолт).
- [ ] Включить модуль на ноде `mytecor-homelab`, прогнать `nix flake check`.
- [ ] Acceptance: из Telegram-чата создать сессию, выполнить реальную задачу с `bash/git/tools`
      (тот же контракт tools, что в f8-03/f8-06), ответить на approval инлайн-кнопкой,
      `/stop` прерывает выполняющийся промпт.
- [ ] Обновить статус f8-06/README F8 и ROADMAP.md (follow-up к закрытой F8).

## Критерий готовности (Definition of Done)

- [ ] С телефона через Telegram-бота выполняется реальная задача на ноде через Pi ACP daemon
      (тот же endpoint `ws://acp.<nodename>.local/`, без второго ACP-клиентского endpoint);
      approvals подтверждаются инлайн-кнопками из Telegram.
- [ ] Telegram-ключи существуют только как agenix-секреты; `nix flake check` проходит,
      сервис переживает перезагрузку ноды без ручных действий.

## Затрагиваемые файлы / слои

- `packages/acprouter/` (новый закреплённый пакет)
- `modules/acprouter/` (новый модуль: `options.nix`, `config.nix`, `default.nix`, `README.md`)
- `nodes/mytecor-homelab/config.nix` (включение модуля, agenix-секреты Telegram)
- `nodes/mytecor-homelab/secrets/` (новые `.age`-файлы Telegram-ключей, генерация через agenix
  вне контекста LLM)
- roadmap: `roadmap/f8-pi-runtime/README.md`, `ROADMAP.md`

## Открытые вопросы

- Совместим ли `acpkit`/`acpremote`-мост из README acprouter с формой соединения Ferngeist
  (WebSocket + subprotocol `acp.v1`, без path/session-URL)? Если нет — нужен ли собственный
  минимальный stdio→WebSocket мост (по образцу обсуждаемого Zed-shim) и не закрывает ли он
  заодно отложенный stdio shim для Zed.
- Куда пишется `ACPROUTER_STATE_DIR` (bindings чатов к сессиям): `StateDirectory` ноды или
  workspace-каталог; входит ли state в `environment.persistence` ноды.
- Кому принадлежит Telegram API id/hash и bot token (личный аккаунт владельца); как secret
  попадает в agenix без раскрытия значения в истории shell.
- Достаточно ли чтения публичных чатов, или бизнес-connection (`ACPROUTER_TELEGRAM_BUSINESS_CONNECTION_ID`)
  нужен для приватности ответов — уточнить на acceptance.
