{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.llm-gateway;
  activeProviders = lib.filterAttrs (_: provider: provider.enable) cfg.providers;

  publicProvider = _name: provider: {
    inherit (provider) id priority headers;
    name = provider.accessGroup;
    base_provider = provider.baseProvider;
    inference_url = provider.inferenceUrl;
    models_url = provider.modelsUrl;
    api_key = null;
    models_api_key = null;
    cooldown = provider.cooldown;
    request_timeout = provider.requestTimeout;
    bifrost_max_retries = provider.bifrostMaxRetries;
    allow_private_network = provider.allowPrivateNetwork;
  };

  publicModel = model: {
    match = { provider = model.accessGroup; id = model.native; };
    override.id = model.logical;
  };

  publicRule = rule: {
    match.model = rule.model;
    inherit (rule) action providers attempts on;
    backoff = {
      type = rule.backoffType;
      initial = rule.backoffInitial;
      max = rule.backoffMax;
    };
    inherit (rule) duration after;
    fallback_strategy = rule.fallbackStrategy;
  };

  publicConfig = {
    inherit (cfg) host port;
    client_api_key = null;
    catalog_refresh_interval = cfg.catalogRefreshInterval;
    providers = lib.mapAttrsToList publicProvider activeProviders;
    models = map publicModel cfg.models;
    routing_rules = map publicRule cfg.routingRules;
  };

  publicConfigFile = pkgs.writeText "llm-gateway-public-config.json" (builtins.toJSON publicConfig);
  dataDir = "/run/${cfg.runtimeDirectory}";
  runtimeConfigFile = "${dataDir}/config.json";

  sanitizeName = name: lib.replaceStrings [ "." "_" ] [ "-" "-" ] name;

  providerCredentials = lib.concatLists (lib.mapAttrsToList
    (name: provider:
      lib.optional (provider.apiKeyFile != null) {
        file = provider.apiKeyFile;
        name = "provider-${sanitizeName name}-api-key";
        targetId = provider.id;
        field = "api_key";
      }
      ++ lib.optional (provider.modelsApiKeyFile != null) {
        file = provider.modelsApiKeyFile;
        name = "provider-${sanitizeName name}-models-api-key";
        targetId = provider.id;
        field = "models_api_key";
      })
    activeProviders);

  runtimeCredentials = providerCredentials;
  loadCredentials =
    lib.optional (cfg.clientCredentialFile != null) "client-key:${cfg.clientCredentialFile}"
    ++ map (credential: "${credential.name}:${credential.file}") runtimeCredentials;

  appendCredential = credential: ''
    next="$tmp.next"
    ${pkgs.jq}/bin/jq \
      --arg id ${lib.escapeShellArg credential.targetId} \
      --arg field ${lib.escapeShellArg credential.field} \
      --rawfile raw "$CREDENTIALS_DIRECTORY/${credential.name}" '
        ($raw | sub("[\\r\\n]+$"; "")) as $secret
        | if ($secret == "" or ($secret | test("[\\r\\n]")))
          then error("invalid provider credential") else . end
        | .providers |= map(if .id == $id then .[$field] = $secret else . end)
      ' "$tmp" > "$next"
    mv "$next" "$tmp"
  '';

  runtimeConfigBuilder = ''
    set -eu
    umask 077
    install -d -m 0700 ${lib.escapeShellArg dataDir}
    tmp=$(mktemp ${lib.escapeShellArg "${runtimeConfigFile}.XXXXXX"})
    trap 'rm -f "$tmp" "$tmp.next"' EXIT

    ${if cfg.clientCredentialFile != null then ''
      ${pkgs.jq}/bin/jq \
        --arg field "client_api_key" \
        --rawfile raw "$CREDENTIALS_DIRECTORY/client-key" '
          ($raw | sub("[\\r\\n]+$"; "")) as $secret
          | if ($secret == "" or ($secret | test("[\\r\\n]")))
            then error("invalid client credential") else . end
          | .[$field] = $secret
        ' ${publicConfigFile} > "$tmp"
    '' else ''
      cp ${publicConfigFile} "$tmp"
    ''}

    ${lib.concatMapStringsSep "\n" appendCredential runtimeCredentials}
    chmod 0600 "$tmp"
    mv "$tmp" ${lib.escapeShellArg runtimeConfigFile}
  '';

  providerIds = map (provider: provider.id) (builtins.attrValues activeProviders);
  providerGroups = lib.unique (map (provider: provider.accessGroup) (builtins.attrValues activeProviders));
  modelKeys = map (model: "${model.logical}:${model.accessGroup}") cfg.models;
  mappedLogicalModels = lib.unique (map (model: model.logical) cfg.models);
  ruleModels = lib.unique (map (rule: rule.model) cfg.routingRules);
  ruleProviderIds = lib.concatMap (rule: rule.providers) cfg.routingRules;
in
{
  config = lib.mkIf cfg.enable {
    lattice.llm-gateway.publicConfigFile = publicConfigFile;

    assertions = [
      {
        assertion = activeProviders != { };
        message = "Bifrost runtime requires at least one enabled provider.";
      }
      {
        assertion = builtins.length providerIds == builtins.length (lib.unique providerIds);
        message = "Bifrost provider IDs must be unique.";
      }
      {
        assertion = builtins.length modelKeys == builtins.length (lib.unique modelKeys);
        message = "Each logical model may have only one primary per access group.";
      }
      {
        assertion = lib.all (model: builtins.elem model.accessGroup providerGroups) cfg.models;
        message = "Every Bifrost model mapping must reference an enabled provider access group.";
      }
      {
        assertion = lib.all (model: builtins.elem model mappedLogicalModels) ruleModels;
        message = "Bifrost routing rules must reference mapped logical models.";
      }
      {
        assertion = lib.all (id: builtins.elem id providerIds) ruleProviderIds;
        message = "Bifrost routing rules must reference enabled provider IDs.";
      }
      {
        assertion = lib.all
          (provider: provider.modelsApiKeyFile == null || provider.modelsUrl != null)
          (builtins.attrValues activeProviders);
        message = "modelsApiKeyFile requires modelsUrl.";
      }
    ];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
    };

    systemd.services.llm-gateway = {
      description = "Lattice OpenAI-compatible LLM gateway";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      preStart = runtimeConfigBuilder;
      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        WorkingDirectory = dataDir;
        LoadCredential = loadCredentials;
        ExecStart = "${lib.getExe cfg.package} --config ${runtimeConfigFile} serve";
        Restart = "on-failure";
        RestartSec = 5;
        UMask = "0077";

        AmbientCapabilities = "";
        CapabilityBoundingSet = "";
        LockPersonality = true;
        # Bifrost's Sonic/Base64x dependency loads SIMD routines at startup with
        # mprotect(PROT_EXEC), so systemd's W^X policy would crash the gateway.
        MemoryDenyWriteExecute = false;
        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProcSubset = "pid";
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHome = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProtectSystem = "strict";
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
      };
    };
  };
}
