# Lattice LLM Gateway

Небольшой OpenAI-compatible HTTP proxy на Go. Он использует
[Bifrost Core](https://github.com/maximhq/bifrost) через Go API как provider execution layer, но
сам владеет клиентским API, logical models, model discovery и routing policy Lattice.

Пакет не содержит встроенных provider names, logical model IDs или mappings. Они полностью
задаются конфигурацией конкретного deployment. Клиенты видят только объявленные logical models и
не получают native model IDs, provider identities, внутренние URLs или provider credentials.

## API

| Endpoint | Назначение |
| --- | --- |
| `GET /healthz` | Минимальный health check без раскрытия topology |
| `GET /v1/models` | Только logical models, настроенные в `models` |
| `POST /v1/chat/completions` | OpenAI Chat Completions, streaming и non-streaming |
| `POST /v1/responses` | OpenAI Responses API, streaming и non-streaming |
| `POST /admin/models/refresh` | Немедленное обновление внутренних model catalogs |

Если `client_api_key` непустой, все `/v1/*` и `/admin/*` endpoints требуют:

```text
Authorization: Bearer <client_api_key>
```

`/healthz` намеренно возвращает только `{"status":"ok"}`.

## Routing

`routing_rules` — плоский упорядоченный pipeline. Каждый rule выполняет одно действие и
преобразует route, созданный предыдущими rules:

- `race` одновременно вызывает перечисленные provider instances;
- `retry` повторяет предыдущий route для выбранных error classes;
- `fallback` добавляет следующий serial/race/hedge stage;
- `timeout` ограничивает предыдущий stage;
- `hedge` запускает дополнительные providers с задержкой `after`.

Для `race` все перечисленные в rule providers запускаются параллельно. Ошибка одной ветки не
завершает запрос: gateway продолжает ждать остальные. Первый успешный ответ побеждает, а
оставшиеся calls отменяются через `context.Context`.

В streaming победитель выбирается только по первому meaningful content, reasoning или tool-call
event. Пустые role/metadata chunks не выигрывают. Prelude выбранной ветки буферизуется и затем
отдаётся клиенту в исходном порядке; проигравшие streams отменяются.

`attempts` означает число повторов после первоначального запуска. Поэтому, например,
`attempts: 10` даёт максимум 11 race-волн. Error classes и backoff также задаются rule.

Provider с retryable failure получает cooldown, по умолчанию 15 секунд. Пока в route есть другой
доступный provider, охлаждаемая ветка пропускается. Если охлаждаются все providers, gateway снова
пробует всю группу вместо искусственного полного простоя.

## Model discovery

`inference_url` и `models_url` независимы. Например, provider без собственного model catalog может
использовать каталог другого совместимого endpoint:

```text
provider-a.inference_url → https://inference-a.example
provider-a.models_url    → https://catalog.example/v1/models
provider-b.inference_url → https://inference-b.example
provider-b.models_url    → отсутствует
```

`models_api_key` задаётся отдельно от inference credential. Gateway не переиспользует
`api_key` для другого host неявно.

Catalog обновляется каждые 10 минут или вручную. Неуспешный refresh сохраняет last-known-good
snapshot. Если primary model исчезла, gateway детерминированно выбирает первую доступную модель из
той же access group после нормализации, дедупликации и сортировки. Пустая группа работает
fail-closed.

## Пример конфигурации: Gonka

Standalone binary поддерживает literal secrets и ссылки `env.VARIABLE_NAME`. NixOS module вместо
этого собирает приватный `/run/llm-gateway/config.json` через systemd `LoadCredential`. Ни имена
`gonka-*`, ни модели `stupid`/`standard` не являются требованиями package — это только пример
конкретного deployment.

```json
{
  "host": "127.0.0.1",
  "port": 9208,
  "client_api_key": "env.LLM_GATEWAY_CLIENT_KEY",
  "catalog_refresh_interval": "10m",
  "providers": [
    {
      "id": "gonka-proxy",
      "name": "gonka",
      "base_provider": "openai",
      "inference_url": "https://proxy.gonka.gg",
      "api_key": "env.PROXY_GONKA_GG_API_KEY",
      "priority": 10,
      "cooldown": "15s",
      "request_timeout": "60s"
    },
    {
      "id": "gonka-openbroker",
      "name": "gonka",
      "base_provider": "openai",
      "inference_url": "https://api.openbroker.gonka.gg",
      "models_url": "https://proxy.gonka.gg/v1/models",
      "api_key": "env.OPENBROKER_GONKA_GG_API_KEY",
      "models_api_key": "env.PROXY_GONKA_GG_API_KEY",
      "priority": 10,
      "cooldown": "15s",
      "request_timeout": "60s"
    }
  ],
  "models": [
    {
      "match": {"provider": "gonka", "id": "MiniMaxAI/MiniMax-M2.7"},
      "override": {"id": "stupid"}
    },
    {
      "match": {"provider": "gonka", "id": "deepseek-ai/DeepSeek-V4-Flash-0731"},
      "override": {"id": "standard"}
    }
  ],
  "routing_rules": [
    {
      "match": {"model": "stupid"},
      "action": "race",
      "providers": ["gonka-proxy", "gonka-openbroker"]
    },
    {
      "match": {"model": "stupid"},
      "action": "retry",
      "attempts": 10,
      "on": ["429", "5xx", "timeout", "connection_error"],
      "backoff": {"type": "exponential", "initial": "100ms", "max": "1s"}
    },
    {
      "match": {"model": "standard"},
      "action": "race",
      "providers": ["gonka-proxy", "gonka-openbroker"]
    },
    {
      "match": {"model": "standard"},
      "action": "retry",
      "attempts": 10,
      "on": ["429", "5xx", "timeout", "connection_error"],
      "backoff": {"type": "exponential", "initial": "100ms", "max": "1s"}
    }
  ]
}
```

## Запуск

```sh
go run . --config /path/to/config.json serve
```

или через Nix package:

```sh
nix build .#packages.x86_64-linux.llm-gateway
```

На NixOS runtime настраивается через
[`modules/llm-gateway`](../../modules/llm-gateway/README.md). Homelab mapping находится в
[`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix).

## Проверка

```sh
go test ./...
go test -race ./...
nix flake check --no-build
```

Тесты покрывают:

- реальный Bifrost custom-provider path для Chat и Responses;
- оба streaming API;
- параллельный старт, first-success и игнорирование первой ошибки;
- cancellation проигравшего provider и client disconnect;
- retry, fallback, timeout, hedge, priority и cooldown;
- independent discovery URL, manual refresh, last-known-good и fail-closed;
- logical/native model rewrite и удаление Bifrost routing metadata из client responses.

## Безопасность

- Provider secrets не входят в публичный Nix config и Git.
- Runtime config имеет mode `0600` и находится в `/run/llm-gateway`.
- Bifrost content logging не включён; executor использует silent logger. Собственные
  структурированные логи gateway не содержат request body, prompt, headers или credentials.
- Ошибки клиенту содержат только безопасный error class, без internal URL, key или native ID.
- Provider-facing raw OpenAI body получает native model только после проверки logical model.

Полный план и незавершённые шаги cutover находятся в
[`f7-07-bifrost-go-proxy.md`](../../docs/roadmap/tasks/f7-07-bifrost-go-proxy.md).
