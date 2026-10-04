# agentrun-openai module

`lattice.agentrun-openai` запускает
[`packages/agentrun-openai`](../../packages/agentrun-openai) — OpenAI-compatible
HTTP-шлюз над библиотекой `github.com/dmora/agentrun`
(форк [`github.com/mytecor/agentrun`](https://github.com/mytecor/agentrun)).
Шлюз предоставляет persistent-сессии agent CLI как модели OpenAI API:

- `claude-code`, `codex`, `agy` — backend-умолчания;
- `claude-code/<model>` и `codex/<model>` — явный выбор модели.

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
| `defaultCwd` | `null` | Рабочая директория сессий по умолчанию. |
| `allowedRoots` | прочее | Разрешённые корни `X-Agent-CWD` (по умолчанию любые). |
| `path` | прочее | Пакеты на PATH каждого спавняемого agent CLI. |
| `extraEnv` | прочее | Доп. переменные окружения. |

## Секреты

Bearer-ключ (если нужен) подаётся файлом через `apiKeyFile` → systemd
`LoadCredential`, в Nix store не попадает (имя опции — путь к узловому
agenix-секрету). По умолчанию шлюз без аутентификации: держите его на loopback
или перед Caddy с credential/ForwardAuth.

## Пример

```nix
{
  lattice.agentrun-openai = {
    enable = true;
    port = 8787;
    apiKeyFile = config.age.secrets.agentrun-openai-key.path;
    # Агентские CLI на PATH сессий.
    path = [ pkgs.claude-code pkgs.codex-acp ];
  };
}
```

Runtime и сессии agent CLI аутентифицируются самостоятельно в процессе gateway
(по `HOME`-каталогам пользователя `lattice.agentrun-openai.user`). См.
[`profiles/agentrun-openai`](../../profiles/agentrun-openai/README.md).
