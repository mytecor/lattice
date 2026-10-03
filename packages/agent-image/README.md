# OCI Agent Runtime Image

Пакет собирает воспроизводимый immutable OCI-образ агентского рантайма
через `pkgs.dockerTools.buildLayeredImage` для выполнения задач в `r1s`
([f10-04](../../roadmap/f10-disposable-worker/f10-04-agent-runtime-acp.md)).

## Контракт образа

Образ полностью immutable и stateless: не содержит task state, credentials или
фиксированного Git-репозитория. Все входные параметры передаются через
переменные окружения при старте контейнера.

### Включает в себя

- `git` — операции с рабочим деревом;
- `Pi` ([`packages/pi`](../pi/README.md)) — coding agent;
- `pi-acp` ([`packages/pi-acp`](../pi-acp/README.md)) — ACP stdio adapter для Pi;
- `pi-tool-profile` ([`profiles/pi`](../../profiles/pi/README.md)) — воспроизводимый базовый профиль инструментов (`bash`, `coreutils`, `curl`, `jq`, `ripgrep`…);
- `hydra-acp` ([`packages/hydra-acp`](../hydra-acp/README.md)) — ACP listener daemon;
- `agent-runtime-bootstrap` (`./bootstrap.sh`) — инициализация workspace и запуск ACP listener;
- CA certificates — доступ к внешним TLS-шлюзам (LLM gateway).

### Переменные окружения при запуске

| Переменная | Дефолт | Описание |
| --- | --- | --- |
| `SOURCE_REPO` | `""` | URL или путь к Git-репозиторию для клонирования |
| `SOURCE_REVISION` | `""` | Коммит, ветка или тег для checkout |
| `ACP_PORT` | `55514` | Порт прослушивания ACP WebSocket endpoint |
| `ACP_SECRET` | `lattice-acp-token` | Токен **только для внешнего** клиента: hydra-acp валидирует его по WS subprotocol `hydra-acp-token.<token>`. Внутренние коммуникации daemon↔pi-acp (stdio-пайп) секрета не используют. Дефолт публичный — при доступе из недоверенной сети задавать явно |
| `WORKSPACE_DIR` | `/workspace` | Каталог рабочей области внутри контейнера |
| `LOG_LEVEL` | `info` | Уровень логирования hydra-acp daemon |

Publish-app также принимает `IPFS_API_ADDRESS` (по умолчанию
`/ip4/127.0.0.1/tcp/5001`) и передаёт его в `nerdctl push` явно, не полагаясь
на пользовательский `~/.ipfs/api`.

## Сборка и публикация

Сборка OCI-архива через Nix:

```sh
nix build .#packages.x86_64-linux.agent-image
```

Публикация в IPFS через nerdctl:

```sh
nix run .#publish-agent-image
```

Скрипт публикует образ в локальный Kubo daemon, разрешает итоговый OCI manifest digest
и сохраняет артефакт публикации `result/publication.json` с CID и digest:

```json
{
  "cid": "bafy...",
  "digest": "sha256:...",
  "reference": "127.0.0.1:5050/ipfs/<CID>@sha256:<digest>"
}
```

Холодный pull через локальный фасад реестра:

```sh
nix run .#pull-agent-image -- 127.0.0.1:5050/ipfs/<CID>@sha256:<digest>
```
