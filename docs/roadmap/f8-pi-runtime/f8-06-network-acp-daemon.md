# Опубликовать Pi как постоянный multi-session ACP daemon

Фича: [F8 — интерактивный Pi runtime](./README.md). Зависит от
[f8-02](./f8-02-pi-gateway-config.md) и
[f8-03](./f8-03-reproducible-tool-profile.md).

## Контекст

Pi должен быть доступен не только из локального TUI, но и как постоянный сетевой ACP-сервис для
Ferngeist, Zed и других ACP-клиентов. Один раз настроенный клиент подключается к
`ws://acp.<nodename>.local/`,
создаёт несколько параллельных Pi-сессий и позже подключается к любой из них без отдельного URL,
room или серверного конфига на каждую сессию.

Зафиксированная базовая архитектура:

```text
Ferngeist ───────────────┐
Zed → локальный shim ────┼── Caddy → hydra-acp daemon → pi-acp → Pi
другой ACP-клиент ───────┘                 │
                                          └── dynamic session registry
```

`hydra-acp` выбран как daemon и владелец registry/agent processes; `pi-acp` остаётся ACP-адаптером
для Pi, а Caddy задаёт LAN ingress. Версии и исходники обоих компонентов
должны быть закреплены декларативно: `hydra-acp` считается experimental dependency, а не
автоматически обновляемым системным пакетом.

Это интерактивные долгоживущие сессии. Они не заменяют stateless RPC-контракт из
[f8-05](./f8-05-pi-rpc-contract.md) и disposable lifecycle из
[F10](../f10-disposable-worker/README.md).

## Что сделать

- [x] Закрепить проверенные версии и hashes `hydra-acp` и `pi-acp`; документировать источник,
      лицензию и процедуру обновления. — [`packages/hydra-acp`](../../../packages/hydra-acp/README.md)
      (`0.1.183`, last-pre-cutoff) и [`packages/pi-acp`](../../../packages/pi-acp/README.md)
      (`0.1.0-unstable-2026-09-09`, commit `34865ae…`).
- [x] Проверить на закреплённых версиях требуемый ACP-контракт: `session/new`, `session/list`,
      `session/attach`/`session/detach`, resurrection cold session, несколько клиентов одной live
      session, broadcast notifications, сериализацию prompts и разрешение одновременных permission
      responses. — [`tests/hydra-acp-smoke.mjs`](../../../tests/hydra-acp-smoke.mjs) и
      [`tests/acp-ingress-smoke.mjs`](../../../tests/acp-ingress-smoke.mjs).
- [x] Добавить декларативный systemd-сервис `hydra-acp`, который слушает только loopback,
      запускает локально закреплённый `pi-acp` без зависимости от плавающего ACP Registry, использует
      тот же Pi package/model/tool profile, что и локальный runtime, и не завершает тихую сессию по
      idle timeout. — [`modules/pi-acp-daemon`](../../../modules/pi-acp-daemon/README.md).
- [x] Опубликовать один стабильный LAN endpoint `ws://acp.<nodename>.local/` через Caddy; внутренний
      Hydra path `/acp` не попадает в клиентскую конфигурацию, а session identity передаётся внутри
      ACP, а не кодируется в URL, room или отдельном reverse-proxy route. —
      [`profiles/tcp-gateway`](../../../profiles/tcp-gateway/README.md).
- [x] Зафиксировать временную границу trusted LAN: endpoint работает без auth и не публикуется во
      внешнем ingress; обязательный внутренний Hydra token не считается credential или защитой.
- [x] Описать один раз настраиваемые способы подключения: прямой LAN WebSocket для Ferngeist —
      описан и проверен тестом; local stdio shim для Zed — зафиксирован отрицательный результат
      stock-клиента и отложен отдельной задачей (см. [Способы подключения](#способы-подключения)).
- [x] Добавить integration/acceptance-проверки создания параллельных сессий, одновременного attach
      двух клиентов, disconnect/reconnect и восстановления session metadata после рестарта daemon.
- [x] Зафиксировать фактическую границу persistence и не объявлять незавершённый turn
      восстановимым без отдельного подтверждения тестом (см. [Границы persistence](#границы-persistence)).

## Критерий готовности

- [x] Два ACP-клиента с неизменным endpoint одновременно подключаются к одной live Pi-сессии,
      видят согласованный поток событий и не получают ответы чужих requests.
- [x] Один клиент создаёт не менее двух параллельных Pi-сессий и переключается между ними через
      session operations без нового URL, room или изменения декларативной конфигурации.
- [x] Отключение всех клиентов не завершает Pi-сессии; после reconnect они снова доступны, а
      проверенное поведение при рестарте daemon/reboot описано в разделе «Границы persistence».
- [x] Документация явно предупреждает, что любой LAN-клиент может управлять Pi; Hydra daemon
      недоступен напрямую, а переход к недоверенной сети блокируется до отдельной auth-задачи.
- [x] Если закреплённая версия `hydra-acp` не подтверждает обязательную multi-session/multi-client
      семантику, задача сохраняет воспроизводимый отрицательный результат и возвращается к выбору
      daemon layer вместо скрытой подмены на connection-owned bridge. — семантика подтверждена
      WebSocket-тестами; отдельный воспроизводимый отрицательный результат зафиксирован для
      stock-клиента как local stdio shim (см. ниже).

## Способы подключения

Один раз настраиваемый способ подключения, подтверждённый acceptance-тестом, — **прямой ACP
WebSocket** (`tests/acp-ingress-smoke.mjs` подключается ровно в этой форме соединения):

```text
ws://acp.<nodename>.local/
```

- Клиент открывает WebSocket с единственным subprotocol `acp.v1` и не указывает ни token, ни path,
  ни URL сессии. Session identity задаётся внутри ACP (`session/new`, `session/attach`).
- Так подключается Ferngeist и любой ACP WebSocket-клиент, способный работать с LAN-URL.

`session/list` отдаёт все сессии, включая ещё не получившие ни одного промпта (в upstream `0.1.183`
они скрыты — daemon-сторона получает `includeNonInteractive: !0` через Lattice-патч пакета; см.
[`packages/hydra-acp`](../../../packages/hydra-acp/README.md)). Без этого свежесозданная сессия
была невидима до первого промпта, и переподключающийся клиент плодил новые сессии вместо resume.

### Отрицательный результат: stock `hydra-acp` client как local stdio shim для Zed

Стандартный CLI [`@hydra-acp/cli`](../../../packages/hydra-acp/README.md) (режимы `acp`/`shim`/`cat`)
не годится как local stdio shim против этого endpoint. Воспроизводимо: для не-loopback хоста
клиент отказывается соединяться без credential в `~/.hydra-acp/remotes.json`
(`No cached credentials for <host>`), а такой credential выдаётся только через `/v1/auth/login`,
на который daemon без master password отвечает `403 “No password configured”`. Дополнительно
выбранный Caddy host переписывает любой path во внутренний `/acp`, поэтому HTTP API клиента
(`/v1/auth/login`, `/v1/health`) через ingress недостижимо — несовместимость принципиальна, а не
только из-за отсутствия пароля.

Решение: **Zed отложен** до отдельной auth-задачи (запись в
[BACKLOG.md](../BACKLOG.md)). «Один endpoint на Caddy» остаётся, но обслуживает только чистых
ACP WebSocket-клиентов (Ferngeist). Любой local stdio shim для Zed в этой архитектуре должен
переводить ACP JSON-RPC с stdio на тот же WebSocket-path (форму соединения Ferngeist), а не
прогонять HTTP API гидры.

## Границы persistence

Проверенное поведение (на закреплённых `hydra-acp 0.1.183` / `pi-acp 34865ae`):

- **Потеря клиента / отключение всех клиентов.** Сессия остаётся в daemon: `sessionIdleTimeoutSeconds
  = 0` не завершает тихую сессию. Reconnect через `session/list` + `session/attach` возвращает её
  с историей (`tests/acp-ingress-smoke.mjs`).
- **Рестарт Caddy.** Caddy — статический прокси; на сессии не влияет. mDNS publisher
  (`acp-mdns`) перезапускается systemd (`Restart=always`).
- **Рестарт daemon.** Hydra хранит session metadata в `HYDRA_ACP_HOME`
  (`/var/lib/hydra-acp/sessions/<id>/`) и на старте заново сеет индекс сессий из диска. Проверено
  тестом: после SIGTERM и перезапуска daemon `session/list` снова показывает обе сессии, attach и
  prompt работают.
- **Reboot узла.** `/var/lib/hydra-acp` добавлен в `environment.persistence."/persist"` на
  `mytecor-homelab`, т.е. переживает reboot; поведение после reboot идентично рестарту daemon.
- **Не заявлено:** незавершённый turn, прерванный крахом daemon (не restarted systemd-юнитом), не
  объявляется восстановимым — для этого нет отдельного теста.

## Затрагиваемые файлы / слои

- [`packages/`](../../../packages/README.md) — закреплённые пакеты `hydra-acp` и `pi-acp`.
- [`modules/`](../../../modules/README.md) — опции и systemd unit ACP daemon.
- [`profiles/pi/`](../../../profiles/pi/README.md) — общий Pi package/model/tool contract.
- [`profiles/tcp-gateway/`](../../../profiles/tcp-gateway/README.md) — WebSocket LAN ingress.
- [`nodes/mytecor-homelab/`](../../../nodes/mytecor-homelab/README.md) — включение сервиса на ноде.
- [ARCHITECTURE.md](../../../ARCHITECTURE.md) — границы daemon, adapter, ingress и persistence.
- [DEPLOYMENT.md](../../../DEPLOYMENT.md) — проверка внешнего endpoint и rollback.

## Открытые вопросы

_нет обратимых решений_: выбор `hydra-acp` зафиксирован, multi-session/multi-client семантика
закреплённой версии подтверждена acceptance-тестами. Открыт только Zed-вопрос, вынесенный в
[BACKLOG.md](../BACKLOG.md) как отдельная auth-задача.
