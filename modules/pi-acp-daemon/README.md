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
auth-границу и stdio shim для Zed вынесена в [BACKLOG.md](../../docs/roadmap/BACKLOG.md).

Endpoint нельзя публиковать за пределами доверенной LAN: любой клиент с сетевым доступом может
создать Pi-сессию и использовать доступные ей shell/tools с правами пользователя сервиса.

Профиль ingress находится в
[`profiles/tcp-gateway`](../../profiles/tcp-gateway/README.md), а задача и acceptance-контракт — в
[f8-06](../../docs/roadmap/f8-pi-runtime/f8-06-network-acp-daemon.md).
