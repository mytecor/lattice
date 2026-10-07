# agentrun-openai module

`lattice.agentrun-openai` запускает
[`packages/agentrun-openai`](../../packages/agentrun-openai) — OpenAI-compatible
HTTP-шлюз над библиотекой `github.com/dmora/agentrun`
(форк [`github.com/mytecor/agentrun`](https://github.com/mytecor/agentrun)).
Шлюз предоставляет persistent-сессии любых ACP-совместимых агентов как модели OpenAI API:

- `<backend-id>` — backend-умолчание;
- `<backend-id>/<model-id>` — явный выбор модели.

Здесь важно: agentrun живёт **внутри процесса gateway** и сам запускает agent CLI,
поэтому модуль — это headless systemd-сервис, не интерактивный клиент.

## Конфигурационная граница

Модуль оборачивает флаги CLI. Полный список опций — в
[`options.nix`](./options.nix). Ключевые поля:

| Опция | Значение по умолчанию | Назначение |
| --- | --- | --- |
| `host` | `127.0.0.1` | Loopback-листенер (policy: наружу только через Caddy). |
| `port` | `8787` | Loopback-порт. Нода задаёт зарегистрированный порт. |
| `apiKeyFile` | `null` | Файл-секрет с bearer-ключом; через `LoadCredential` + wrapper. |
| `backends` | `{ codex = { command = "codex-acp"; }; }` | ACP-бэкенды (`--acp`). С `c23d957` effort-варианты авто-обнаруживаются — `--effort-format` удалён. |
| `defaultCwd` | `null` | Рабочая директория сессий по умолчанию. |
| `allowedRoots` | прочее | Разрешённые корни `X-Agent-CWD` (по умолчанию любые). |
| `path` | прочее | Пакеты на PATH каждого спавняемого agent CLI. |
| `extraEnv` | прочее | Доп. переменные окружения. |
| `user` / `group` | `agentrun` | Юзер/группа сервиса. Нода может поставить `root` (как pi-acp-daemon), когда спавняемому агенту нужен общий root-конфиг и agenix-секреты. |
| `protectHome` | `true` | systemd `ProtectHome`: `true` (строго), `"read-only"` (root-конфиг читается агентом), или `null` (выключено). |

## Root-режим и общий Pi-конфиг

Когда бэкенд — `pi-acp`, а гейтвей работает как root (`user = "root"`,
`protectHome = "read-only"`), спавняемый агент читает общий конфиг
[`lattice.pi`](../../modules/pi) из `/root/.pi/agent` (те же llm-gateway-креды и
расширения, что у [`pi-acp-daemon`](../../modules/pi-acp-daemon)) и может
разрешить `!cmd`-ссылку на agenix-секрет через `/run/agenix`. Укажите
`extraEnv.PI_CODING_AGENT_DIR = "/root/.pi/agent"`, сессии держите в stateDir
(`PI_CODING_AGENT_SESSION_DIR`). См. `nodes/mytecor-homelab/config.nix`.

При этом **обязательно** задайте `extraEnv.PI_ACP_DIR` на writable-путь под
stateDir (например `/var/lib/agentrun-openai/pi-acp`): pi-acp по умолчанию
пишет свой `session-map` в `$HOME/.pi/pi-acp` (= `/root/.pi/pi-acp` у root),
а `ProtectHome = "read-only"` монтирует `/root` read-only в namespace
сервиса — `session/new` падает с `ENOENT mkdir …/session-map.json.d`.
Тот же приём, что в [`modules/pi-acp-daemon`](../../modules/pi-acp-daemon)
(`PI_ACP_DIR = stateDir/pi-acp`).

## Секреты

Bearer-ключ (если нужен) подаётся файлом через `apiKeyFile` → systemd
`LoadCredential`, в Nix store не попадает (имя опции — путь к узловому
agenix-секрету). По умолчанию шлюз без аутентификации: держите его на loopback
или перед Caddy с credential/ForwardAuth.

В root-режиме Pi-агент дополнительно читает agenix-секрет llm-gateway
(`/run/agenix/llm-gateway-client-node-pi`, mode `0400`) — поэтому процесс
должен быть root (схема та же, что у `pi-acp-daemon`).

## Пример

```nix
{
  lattice.agentrun-openai = {
    enable = true;
    port = 8787;
    apiKeyFile = config.age.secrets.agentrun-openai-key.path;
    # Агентские CLI / ACP-адаптеры на PATH сессий.
    path = [ pkgs.codex-acp ];
    backends = {
      codex = {
        command = "codex-acp";
      };
      pi = {
        command = "pi-acp";
      };
    };
  };
}
```

Runtime и сессии agent CLI аутентифицируются самостоятельно в процессе gateway
(по `HOME`-каталогам пользователя `lattice.agentrun-openai.user`). См.
[`profiles/agentrun-openai`](../../profiles/agentrun-openai/README.md).
