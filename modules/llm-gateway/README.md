# LLM gateway module

`lattice.llm-gateway` runs the pinned headless `token-proxy` package as an unprivileged systemd
service. Routing, aliases, priorities and endpoint metadata form a public typed Nix configuration.
Only the client key and individual upstream API keys are read at service start through
`LoadCredential`.

The module writes a secret-free JSON template to the Nix store. `ExecStartPre` copies it into the
private runtime directory and injects credentials with `jq`; the resulting `config.jsonc` is mode
`0600` and disappears on reboot. Secret options are runtime path strings, never Nix paths.

When `logicalModels` is non-empty, evaluation rejects prefixed discovery, unknown advertised IDs,
missing logical classes and aliases without an explicit mapping. The standard profile fixes this
contract to `cheap`, `standard`, `strong` and `frontier`.

```nix
{
  lattice.llm-gateway = {
    enable = true;
    clientCredentialFile = config.age.secrets.llm-gateway-client-key.path;
    upstreams.primary = {
      providers = [ "openai" "openai-response" ];
      baseUrl = "https://api.example.test/v1";
      apiKeyFiles = [ config.age.secrets.llm-provider-primary-key.path ];
      availableModels = [ "cheap" "standard" "strong" "frontier" ];
      modelMappings = {
        cheap = "provider-small";
        standard = "provider-medium";
        strong = "provider-large";
        frontier = "provider-frontier";
      };
    };
  };
}
```

The service listens on loopback by default and does not open a firewall port. Account-backed
OAuth providers are not silently converted to static keys: the pinned CLI has no declarative
headless account-import command, so this module currently accepts API-key upstreams only.
