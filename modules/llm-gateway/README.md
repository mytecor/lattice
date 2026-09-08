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
    logLevel = "info";
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
        inferenceUrl = "https://api.openbroker.gonka.gg";
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
      { inherit model; action = "race"; accessGroups = [ "gonka" ]; }
      { inherit model; action = "hedge"; }
      { inherit model; action = "retry"; attempts = 3; on = [ "429" "5xx" "timeout" "connection_error" ]; backoffInitial = "200ms"; backoffMax = "5s"; }
      { inherit model; action = "timeout"; duration = "60s"; }
    ]) [ "stupid" "standard" ];
  };
}
```

Production configuration must provide mappings and rules for every logical model. The service
listens on loopback by default and does not open a firewall port. Caddy remains the only LAN
ingress. The unit keeps systemd hardening enabled except for `MemoryDenyWriteExecute`: Bifrost's
Sonic/Base64x dependency loads SIMD routines with `mprotect(PROT_EXEC)` during process startup.

## Logs

The gateway writes structured JSON to the systemd journal when `logLevel` is one of `error`,
`warn`, `info`, `debug`, or `trace`. The secure default is `silent`. Request bodies,
prompts, headers, and credentials are never logged. At `info`, the journal records request
start/completion with a request ID, logical model, API kind, streaming flag, status, and latency.
At `debug`, it additionally records provider routing, native model, route stage/attempt, and
latency. Upstream failures include HTTP status, error class, and a single-line truncated provider
error message.

```sh
journalctl -u llm-gateway -f -o cat
```
