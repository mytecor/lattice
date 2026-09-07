# LLM gateway module

`lattice.llm-gateway` управляет OpenAI-compatible gateway как непривилегированным systemd
service. Runtime — собственный Go proxy из `packages/llm-gateway`, использующий Bifrost Core
через Go API.

Routing, logical/native mappings, provider identities и независимые inference/discovery URLs
являются открытой typed Nix configuration. Client key, provider inference key и отдельный catalog
key поступают только через `LoadCredential`.

Модуль записывает secret-free JSON template в Nix store. `ExecStartPre` копирует его в закрытый
runtime directory и подставляет credentials через `jq`; итоговый `/run/llm-gateway/config.json`
имеет mode `0600` и исчезает при перезагрузке. Secret options — runtime path strings, не Nix paths.

```nix
{
  lattice.llm-gateway = {
    enable = true;
    clientCredentialFile = config.age.secrets.llm-gateway-client-key.path;

    providers = {
      proxy = {
        id = "gonka-proxy";
        accessGroup = "gonka";
        inferenceUrl = "https://proxy.gonka.gg";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
      };
      openbroker = {
        id = "gonka-openbroker";
        accessGroup = "gonka";
        inferenceUrl = "https://openbroker.gonka.gg";
        modelsUrl = "https://proxy.gonka.gg/v1/models";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-openbroker.path;
        modelsApiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
      };
    };

    models = [
      { logical = "stupid"; accessGroup = "gonka"; native = "MiniMaxAI/MiniMax-M2.7"; }
      { logical = "standard"; accessGroup = "gonka"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
    ];
    routingRules = builtins.concatMap (model: [
      { inherit model; action = "race"; providers = [ "gonka-proxy" "gonka-openbroker" ]; }
      { inherit model; action = "retry"; attempts = 10; on = [ "429" "5xx" "timeout" "connection_error" ]; }
    ]) [ "stupid" "standard" ];
  };
}
```

Production configuration must provide mappings and rules for every logical model. The service
listens on loopback by default and does not open a firewall port. Caddy remains the only LAN
ingress.
