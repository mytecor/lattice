# LLM gateway module

`lattice.llm-gateway` управляет OpenAI-compatible gateway как непривилегированным systemd
service. Runtime — собственный Go proxy из `packages/llm-gateway`, использующий Bifrost Core
через Go API.

Routing, logical/native mappings и provider identities являются открытой typed Nix configuration.
По умолчанию discovery URL выводится как `${inferenceUrl}/models`; `modelsUrl` позволяет задать
независимый источник. Client key, provider inference key и отдельный catalog key поступают только
через `LoadCredential`.

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
        inferenceUrl = "https://proxy.gonka.gg/v1";
        apiKeyFile = config.age.secrets.llm-provider-gonka-gg-proxy.path;
      };
      openbroker = {
        id = "gonka-openbroker";
        accessGroup = "gonka";
        inferenceUrl = "https://api.openbroker.gonka.gg/v1";
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
      { inherit model; action = "pool"; accessGroups = [ "gonka" ]; }
      { inherit model; action = "rank"; strategy = "priority"; }
      {
        inherit model;
        action = "lease";
        source = "winner";
        duration = "10m";
        renewOnSuccess = true;
        releaseOn = [ "429" "5xx" "timeout" "connection_error" ];
        releaseAfterSlowStarts = 3;
        slowStart = "3s";
      }
      {
        inherit model;
        action = "affinity";
        sources = [ "responses.conversation" "responses.previous_response_id" ];
        ttl = "24h";
        onMissing = "ignore";
        onProviderFailure = "fail-closed";
      }
      { inherit model; action = "race"; count = 2; }
      {
        inherit model;
        action = "retry";
        scope = "next";
        count = 1;
        attempts = 2;
        on = [ "429" "5xx" "timeout" "connection_error" "invalid_response" ];
        backoffInitial = "200ms";
        backoffMax = "1s";
      }
      { inherit model; action = "hedge"; after = "3s"; }
      {
        inherit model;
        action = "semaphore";
        maxCalls = 4;
        maxInFlight = 3;
        maxCallsPerProvider = 1;
      }
      { inherit model; action = "timeout"; duration = "60s"; }
    ]) [ "stupid" "standard" ];
  };
}
```

Production configuration must provide mappings and rules for every logical model. Rules follow the
canonical action order `pool → rank → lease → affinity → race → retry → hedge → semaphore →
timeout`; each action owns only its own fields and the gateway rejects unknown or misplaced fields
at startup. Legacy `race access_groups` and `retry` without `scope` are still normalized;
parameterless `hedge` is rejected with a migration error.

The service listens on loopback by default and does not open a firewall port. Caddy remains the only
LAN ingress. The unit keeps systemd hardening enabled except for `MemoryDenyWriteExecute`: Bifrost's
Sonic/Base64x dependency loads SIMD routines with `mprotect(PROT_EXEC)` during process startup.

Responses affinity state (opaque id → provider mapping for `conversation` and
`previous_response_id`) is snapshotted to `/run/llm-gateway/affinity.json` (or `affinityFile` if
set) with mode `0600`, owned by the gateway user; it survives service restarts and is cleared on a
full service stop or reboot. The file contains no prompts, keys, or provider URLs. Failed writes
remain dirty and are retried by the next periodic/final flush; persistence errors are logged.

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
