# LLM gateway module

`lattice.llm-gateway` запускает OpenAI-compatible gateway как непривилегированный
systemd service. Runtime реализован в
[`packages/llm-gateway`](../../packages/llm-gateway/README.md).

## Конфигурационная граница

Модуль намеренно не описывает повторную Nix-схему gateway. `settings` — обычный
attrset с нативными snake_case-полями JSON-конфига. Модуль выполняет только
`builtins.toJSON`; он не переименовывает поля, не генерирует routing pipelines и
не добавляет action-specific defaults.

Единственный источник истины для схемы, defaults и route-валидации — Go runtime.
При сборке модуль запускает:

```sh
llm-gateway --require-env-secrets --allowed-env <declared-names> \
  --config <generated.json> check
```

Поэтому неизвестные поля/actions, неправильный порядок rules, неизвестные
providers, отсутствующие targets и циклы обнаруживаются до активации системы.
Команда `check` не разрешает `env.NAME` и не требует доступа к секретам;
`--require-env-secrets` дополнительно запрещает literal credentials в public config, а
`--allowed-env` проверяет точное совпадение ссылок с ключами `credentials`.

## Пример

```nix
{
  lattice.llm-gateway = {
    enable = true;

    credentials = {
      LATTICE_CLIENT_PI_KEY =
        config.age.secrets.llm-gateway-client-pi.path;
      LATTICE_LLM_PROVIDER_PROXY_KEY =
        config.age.secrets.llm-provider-proxy.path;
    };

    settings = {
      host = "127.0.0.1";
      port = 9208;
      metrics_host = "127.0.0.1";
      metrics_port = 9209;
      log_level = "info";
      catalog_refresh_interval = "10m";
      stream_idle_timeout = "5m";
      affinity_file = "/run/llm-gateway/affinity.json";

      client_api_keys = [
        { id = "pi"; key = "env.LATTICE_CLIENT_PI_KEY"; }
      ];

      providers = [{
        id = "proxy";
        base_provider = "openai";
        inference_url = "https://proxy.example/v1";
        api_key = "env.LATTICE_LLM_PROVIDER_PROXY_KEY";
      }];

      routing_rules = [
        { route = "standard"; action = "filter"; where.model.eq = "standard"; }
        { route = "standard"; action = "filter"; where.provider."in" = [ "proxy" ]; }
        { route = "standard"; action = "map"; native = "native-model"; }
        { route = "standard"; action = "rank"; strategy = "priority"; }
        { route = "standard"; action = "race"; count = 1; }
      ];
    };
  };
}
```

Полное production-описание находится в
[`nodes/mytecor-homelab/config.nix`](../../nodes/mytecor-homelab/config.nix).
Семантика actions и JSON-полей документирована в
[`packages/llm-gateway/README.md`](../../packages/llm-gateway/README.md).

## Секреты

`credentials` отображает имя environment variable на runtime-путь секретного
файла. Публичный JSON содержит только ссылки `env.NAME`.

Отдельный `llm-gateway-env.service` получает файлы через systemd
`LoadCredential`, собирает `/run/llm-gateway-env/keys.env` с mode `0600`, а
gateway читает его через `EnvironmentFile`. Содержимое секретов не попадает в
Nix store или generated JSON.

Имена в `credentials` должны быть валидными environment variable names. Модуль
не выводит имя автоматически из provider/client ID: соответствие всегда видно
непосредственно в конфигурации.

## Runtime files

- `/run/llm-gateway/config.json` — закрытая копия проверенного public config;
- `/run/llm-gateway/affinity.json` — affinity state, если путь задан в settings;
- `/run/llm-gateway-env/keys.env` — materialized runtime credentials.

Gateway работает без root и с systemd hardening. `MemoryDenyWriteExecute=false`
остаётся необходимым для SIMD runtime Bifrost/Sonic.

## Сетевой доступ

Профиль [`profiles/llm-gateway`](../../profiles/llm-gateway/README.md) задаёт
loopback defaults. LAN/mesh ingress предоставляет Caddy из
[`profiles/tcp-gateway`](../../profiles/tcp-gateway/README.md); firewall port для
gateway напрямую не открывается.
