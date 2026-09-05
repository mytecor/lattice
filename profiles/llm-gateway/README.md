## LLM Gateway

Профиль разворачивает [token_proxy](https://github.com/mxyhi/token_proxy) headless как systemd service
с typed NixOS options и agenix secrets.

### Компоненты

- `modules/llm-gateway/` — NixOS модуль с options и config
- `profiles/llm-gateway/` — preset с logical models и routing defaults

### Конфигурация ноды

```nix
{
  imports = [ profiles/llm-gateway ];

  # Secrets подаются через agenix:
  # age.secrets.llm-gateway-client-key (опционально)
  # age.secrets.llm-provider-<name>-key (по одному на upstream)

  lattice.llm-gateway = {
    clientCredentialFile = config.age.secrets.llm-gateway-client-key.path;
    upstreams.proxy.provider-api-key = {
      apiKeyFile = config.age.secrets.llm-provider-proxy.path;
      # ...
    };
  };
}
```

### Generating secrets

```sh
# Generate age-encrypted API key
agenix -e <name>.age -i /path/to/recovery-key
# Press Enter, type key, Ctrl+D
```

See [KEY_MANAGEMENT.md](../../KEY_MANAGEMENT.md) for rotation workflow.
