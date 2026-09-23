# Pi ACP daemon module

Модуль `lattice.pi-acp-daemon` запускает закреплённый Hydra ACP как foreground systemd service.
Hydra слушает только loopback, хранит session metadata в `StateDirectory` и создаёт каждую сессию
через локальный [`pi-acp`](../../packages/pi-acp/README.md). Плавающий ACP Registry для Pi не
используется.

Текущий LAN endpoint намеренно не имеет authentication. Опция `internalToken` — публичная
фиксированная строка для обязательного Caddy-to-Hydra handshake, а не credential или граница
доступа. Hydra получает её через файл в `RuntimeDirectory`; Caddy добавляет строку только к
внутреннему loopback query.

Внешний LAN endpoint — `ws://acp.<nodename>.local/`. Обязательный для Hydra путь `/acp` остаётся
внутренней деталью loopback upstream и добавляется Caddy через rewrite.

## PATH агента и tool profile

Опция `lattice.pi-acp-daemon.path` (listOf package) задаёт пакеты/derivations, чьи `bin`
**дописываются в начало** `PATH` каждого спавняемого Pi-агента (через сгенерированный Hydra
`agents.pi-acp.env`). Дефолтный systemd-PATH сервиса (NixOS) не содержит ни одной директории с
`sh`, поэтому без этой опции bash-инструмент Pi падает с `spawn sh ENOENT` — `sh` ищется по имени
на PATH агента, а не абсолютным путём. Передайте сюда tool profile
(`pkgs.lattice.pi-tool-profile`), чтобы демон давал сессиям тот же `bash/git/tools`-контракт, что и
локальный runtime (f8-03).

В конец PATH всегда дописывается системный профиль NixOS (`/run/current-system/sw/bin` и
`/run/current-system/sw/sbin`), чтобы у агента оставались системные утилиты ноды (`nix` и
остальные `systemPackages`) — иначе замена PATH только tool profile лишила бы сессии доступа к
`nix`, `nixos-rebuild` и т.п. Благодаря `daemon.scrubEnv = []` этот override доходит до
`pi --mode rpc` без изменений.

```nix
# nodes/<node>/config.nix
lattice.pi-acp-daemon.path = [ pkgs.lattice.pi-tool-profile ];
```

## Окружение сессий (extraEnv)

Опция `lattice.pi-acp-daemon.extraEnv` (attrsOf str, по умолчанию `{}`) задаёт дополнительные
переменные окружения, которые сливаются в `agents.pi-acp.env` каждого спавняемого агента. Нужно
для переменных, которые должны доходить до сессии и её Git-процессов (remote helpers читают
окружение), но которые не являются пакетами на PATH.

f15-02: нода задаёт `RAD_HOME` в свой Radicle peer-профиль
(`/persist/var/lib/radicle-peer`), поэтому `git push rad://...` из workspace подписывается
peer-идентичностью ноды (а не seed-профилем rad-system):

```nix
lattice.pi-acp-daemon.extraEnv.RAD_HOME = "/persist/var/lib/radicle-peer";
```

Проверяется контракт-тестом [`tests/pi-acp-daemon.nix`](../../tests/pi-acp-daemon.nix).

## Рабочая директория сессий (defaultCwd)

Опция `lattice.pi-acp-daemon.defaultCwd` (nullOr str, по умолчанию `null`) задаёт рабочую
директорию для новых ACP-сессий. При `null` сессии стартуют в домашней директории
пользователя сервиса (`/root` по умолчанию). Укажите её
на рабочий checkout (например `/var/lib/lattice-workspace/lattice`, f15-01), чтобы агент в
сессии открывался прямо в рабочей копии репозитория на ноде:

```nix
lattice.pi-acp-daemon.defaultCwd = "/var/lib/lattice-workspace/lattice";
```

Значение попадает в сгенерированный Hydra-конфиг (`daemon.defaultCwd`) и проверяется
контракт-тестом [`tests/pi-acp-daemon.nix`](../../tests/pi-acp-daemon.nix).

**Как это работает.** В WS `session/new` схема требует поле `cwd` (`K.string()`, не
опциональное), и upstream-демон передаёт его как есть в `manager.create` →
`resolveWorkspace` → `bootstrapAgent` — то есть спавнит агента ровно в переданном клиентом
каталоге, не подставляя серверный дефолт. Поэтому для node dev-loop серверного
`defaultCwd` самого по себе недостаточно: stateless-клиент (acp-ui, Ferngeist) присылает
свой cwd (пустую строку, `/` или иной путь) и увёл бы сессию из рабочего checkout.

Lattice-патч в [`packages/hydra-acp`](../../packages/hydra-acp/package.nix) решает это на
сервере: `manager.create` **безусловно** заменяет cwd клиента на `fe(this.defaultCwd)`
(`expandHome`, та же, что в `resolveResurrectTarget`) — любая сессия открывается в рабочем
checkout, каким бы путём клиент ни попытался её направить. Явный путь из `session/new`
клиентом не уважается: на этой ноде всё ACP-сессии работают в единой рабочей копии.
Клиентским патчам (например, правкам acp-components) здесь места нет — поведение задаётся
только серверной политикой.

Патч покрывает и чтение списка сессий: `session/list` также **безусловно** фильтрует по
`fe(e.manager.defaultCwd)`, игнорируя cwd, присланный клиентом. Иначе серверный `defaultCwd`
работал бы только на создание: лист-фильтр демона сопоставляет `session.cwd` с cwd из запроса
(путь-равенство `Td`/`wo`), и stateless-клиент, запрашивающий список с чужим путём (например
`/` — то, что acp-ui шлёт после обновления страницы), скрыл бы все сессии из рабочего
checkout даже при корректно созданных сессиях. Та же серверная политика, что и для создания, —
листинг ведётся только по рабочей копии. Регрессионная проверка — `session/list` с cwd = `/`
в [`tests/acp-ingress-smoke.mjs`](../../tests/acp-ingress-smoke.mjs) и
[`tests/hydra-acp-smoke.mjs`](../../tests/hydra-acp-smoke.mjs).

## Временный privileged-доступ (stopgap, переработать!)

Опция `lattice.pi-acp-daemon.privileged` (bool, по умолчанию `false`) — **временная** мера для
живой диагностики сети (iw/ip/nl80211) из сессий Pi. При `true` сервис:

- добавляет `AF_NETLINK` в `RestrictAddressFamilies` — открывается netlink, заработают
  `iw`/`ip`-запросы, которые раньше падали с «Address family not supported» /
  «Failed to connect to generic netlink»;
- выдаёт `CAP_NET_ADMIN` (и `CAP_NET_RAW`, `CAP_NET_BIND_SERVICE`, `CAP_DAC_OVERRIDE`,
  `CAP_SYS_ADMIN`, `CAP_SETUID`, `CAP_SETGID`) в `AmbientCapabilities`/`CapabilityBoundingSet`;
- снимает `NoNewPrivileges` — работают `sudo`/`setuid`;
- ослабляет `ProtectSystem`/`PrivateDevices`.

**Это stopgap, а не целевая конфигурация.** Строгий песочник (restricted address families без
netlink, пустой `CapabilityBoundingSet` → `CapEff=0`, `NoNewPrivileges=true`, `ProtectSystem=full`,
`PrivateDevices`) — то, к чему сервис должен вернуться после завершения диагностики. Задача на
возврат к минимальным доступам зафиксирована в [BACKLOG.md](../../roadmap/BACKLOG.md). Не
включайте `privileged` на недоверенной LAN: сессия становится root-процессом с сетевым админом.

## Transformers

Опции `lattice.pi-acp-daemon.transformers` (attrset с `command`/`args`/`env`/`enabled`) и
`defaultTransformers` (list of names) передаются в сгенерированный Hydra-конфиг как есть:
daemon спавнит каждый transformer процесс (майнит per-process transformer-токен и кладёт его
в `HYDRA_ACP_TOKEN`/`HYDRA_ACP_WS_URL`), а `defaultTransformers` добавляет их в цепочку **всех**
новых сессий. Без `defaultTransformers` зарегистрированный transformer подключает только сессия,
которая явно запросила его в `session/new` через `_meta: { "hydra-acp": { "transformers": ["<name>"] } }`
(цепочка фиксируется при создании сессии).

Lattice-owned transformer — [`packages/acp-normalizer`](../../packages/acp-normalizer/README.md):
переприсваивает per-token `messageId` на `agent_message_chunk`/`agent_thought_chunk` стабильным id в
рамках одного логического ассистентского сообщения. Включён на `mytecor-homelab` через
[`nodes/mytecor-homelab`](../../nodes/mytecor-homelab/README.md).

## Форма соединения клиента

Единственная проверенная форма клиентского подключения — чистый ACP WebSocket с subprotocol
`acp.v1` и без token/path/URL сессии (сессия выбирается внутри ACP). Так подключается Ferngeist;
эту же форму соединения покрывает `tests/acp-ingress-smoke.mjs`.

Stock-клиент `hydra-acp` (`acp`/`shim`/`cat`) для этого endpoint **не подходит**: он требует
login-credential для не-loopback хоста, который без master password у daemon получить нельзя, а
Caddy-rewrite любого path во внутренний `/acp` делает HTTP API клиента недостижимым. Задача на
auth-границу и stdio shim для Zed вынесена в [BACKLOG.md](../../roadmap/BACKLOG.md).

Endpoint нельзя публиковать за пределами доверенной LAN: любой клиент с сетевым доступом может
создать Pi-сессию и использовать доступные ей shell/tools с правами пользователя сервиса.

Профиль ingress находится в
[`profiles/tcp-gateway`](../../profiles/tcp-gateway/README.md), а задача и acceptance-контракт — в
[f8-06](../../roadmap/f8-pi-runtime/f8-06-network-acp-daemon.md).
