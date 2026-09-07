{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.llm-gateway;
  useBifrost = cfg.runtime == "bifrost";
  activeUpstreams = lib.filterAttrs (_: upstream: upstream.enable) cfg.upstreams;
  activeProviders = lib.filterAttrs (_: provider: provider.enable) cfg.providers;

  dispatch =
    if cfg.routing.dispatch == "serial" then
      { type = "serial"; }
    else if cfg.routing.dispatch == "hedged" then {
      type = "hedged";
      delay_ms = cfg.routing.hedgeDelayMs;
      max_parallel = cfg.routing.maxParallel;
    } else {
      type = "race";
      max_parallel = cfg.routing.maxParallel;
    };

  publicUpstream = _name: upstream: {
    inherit (upstream) id priority providers;
    base_url = upstream.baseUrl;
    available_models = upstream.availableModels;
    model_mappings = upstream.modelMappings;
    convert_from_map = upstream.convertFrom;
    proxy_url = upstream.proxyUrl;
    credential = { type = "api_keys"; api_keys = [ ]; };
    overrides.header = upstream.headerOverrides;
  };

  legacyPublicConfig = {
    inherit (cfg) host port;
    local_api_key = null;
    log_level = cfg.logLevel;
    model_list_prefix = cfg.modelListPrefix;
    model_list_prefix_default_on_migrated = true;
    retryable_failure_cooldown_secs = cfg.retryableFailureCooldownSeconds;
    same_upstream_retry_count = cfg.sameUpstreamRetryCount;
    upstream_strategy = { order = cfg.routing.order; inherit dispatch; };
    hot_model_mappings = { };
    upstreams = lib.mapAttrsToList publicUpstream activeUpstreams;
  };

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

  bifrostPublicConfig = {
    inherit (cfg) host port;
    client_api_key = null;
    catalog_refresh_interval = cfg.catalogRefreshInterval;
    providers = lib.mapAttrsToList publicProvider activeProviders;
    models = map publicModel cfg.models;
    routing_rules = map publicRule cfg.routingRules;
  };

  publicConfig = if useBifrost then bifrostPublicConfig else legacyPublicConfig;
  publicConfigFile = pkgs.writeText "llm-gateway-public-config.json" (builtins.toJSON publicConfig);
  dataDir = "/run/${cfg.runtimeDirectory}";
  runtimeConfigFile = "${dataDir}/${if useBifrost then "config.json" else "config.jsonc"}";

  sanitizeName = name: lib.replaceStrings [ "." "_" ] [ "-" "-" ] name;
  legacyCredentialName = upstreamName: index:
    "upstream-${sanitizeName upstreamName}-${toString index}";

  legacyCredentials = lib.concatLists (lib.mapAttrsToList
    (name: upstream: lib.imap0
      (index: file: {
        inherit file;
        name = legacyCredentialName name index;
        targetId = upstream.id;
        field = "legacy";
      })
      upstream.apiKeyFiles)
    activeUpstreams);

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

  runtimeCredentials = if useBifrost then providerCredentials else legacyCredentials;
  loadCredentials =
    lib.optional (cfg.clientCredentialFile != null) "client-key:${cfg.clientCredentialFile}"
    ++ map (credential: "${credential.name}:${credential.file}") runtimeCredentials;

  appendLegacyCredential = credential: ''
    next="$tmp.next"
    ${pkgs.jq}/bin/jq \
      --arg id ${lib.escapeShellArg credential.targetId} \
      --rawfile raw "$CREDENTIALS_DIRECTORY/${credential.name}" '
        ($raw | sub("[\\r\\n]+$"; "")) as $secret
        | if ($secret == "" or ($secret | test("[\\r\\n]")))
          then error("invalid upstream credential") else . end
        | .upstreams |= map(if .id == $id then .credential.api_keys += [$secret] else . end)
      ' "$tmp" > "$next"
    mv "$next" "$tmp"
  '';

  appendProviderCredential = credential: ''
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

  appendCredential = if useBifrost then appendProviderCredential else appendLegacyCredential;

  runtimeConfigBuilder = ''
    set -eu
    umask 077
    install -d -m 0700 ${lib.escapeShellArg dataDir}
    tmp=$(mktemp ${lib.escapeShellArg "${runtimeConfigFile}.XXXXXX"})
    trap 'rm -f "$tmp" "$tmp.next"' EXIT

    ${if cfg.clientCredentialFile != null then ''
      ${pkgs.jq}/bin/jq \
        --arg field ${lib.escapeShellArg (if useBifrost then "client_api_key" else "local_api_key")} \
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

  upstreamIds = map (upstream: upstream.id) (builtins.attrValues activeUpstreams);
  legacyCredentialNames = map (credential: credential.name) legacyCredentials;
  advertisedModels = lib.unique (lib.concatMap
    (upstream: upstream.availableModels)
    (builtins.attrValues activeUpstreams));
  mappingCovers = upstream: model:
    builtins.hasAttr model upstream.modelMappings || builtins.hasAttr "*" upstream.modelMappings;

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
        assertion = useBifrost || activeUpstreams != { };
        message = "token-proxy runtime requires at least one enabled upstream.";
      }
      {
        assertion = useBifrost || lib.all (upstream: upstream.providers != [ ]) (builtins.attrValues activeUpstreams);
        message = "Every enabled token-proxy upstream must declare at least one provider.";
      }
      {
        assertion = useBifrost || lib.all (upstream: upstream.apiKeyFiles != [ ]) (builtins.attrValues activeUpstreams);
        message = "Every enabled token-proxy upstream must provide at least one apiKeyFile.";
      }
      {
        assertion = useBifrost || builtins.length upstreamIds == builtins.length (lib.unique upstreamIds);
        message = "Enabled token-proxy upstream IDs must be unique.";
      }
      {
        assertion = useBifrost || builtins.length legacyCredentialNames == builtins.length (lib.unique legacyCredentialNames);
        message = "Token-proxy upstream names produce duplicate systemd credential names.";
      }
      {
        assertion = useBifrost || cfg.routing.dispatch == "serial" || cfg.routing.maxParallel >= 2;
        message = "race and hedged token-proxy dispatch require maxParallel >= 2.";
      }
      {
        assertion = useBifrost || builtins.length cfg.logicalModels == builtins.length (lib.unique cfg.logicalModels);
        message = "lattice.llm-gateway.logicalModels must not contain duplicates.";
      }
      {
        assertion = useBifrost || cfg.logicalModels == [ ] || !cfg.modelListPrefix;
        message = "A token-proxy logical model contract requires modelListPrefix = false.";
      }
      {
        assertion = useBifrost || cfg.logicalModels == [ ] || lib.all
          (model: builtins.elem model cfg.logicalModels) advertisedModels;
        message = "Token-proxy upstreams may advertise only configured logicalModels.";
      }
      {
        assertion = useBifrost || cfg.logicalModels == [ ] || lib.all
          (model: builtins.elem model advertisedModels) cfg.logicalModels;
        message = "Every token-proxy logical model must be advertised by an upstream.";
      }
      {
        assertion = useBifrost || cfg.logicalModels == [ ] || lib.all
          (upstream: lib.all (mappingCovers upstream) upstream.availableModels)
          (builtins.attrValues activeUpstreams);
        message = "Every advertised token-proxy logical model must map to a native model ID.";
      }
      {
        assertion = !useBifrost || activeProviders != { };
        message = "Bifrost runtime requires at least one enabled provider.";
      }
      {
        assertion = !useBifrost || builtins.length providerIds == builtins.length (lib.unique providerIds);
        message = "Bifrost provider IDs must be unique.";
      }
      {
        assertion = !useBifrost || builtins.length modelKeys == builtins.length (lib.unique modelKeys);
        message = "Each logical model may have only one primary per access group.";
      }
      {
        assertion = !useBifrost || lib.all (model: builtins.elem model.accessGroup providerGroups) cfg.models;
        message = "Every Bifrost model mapping must reference an enabled provider access group.";
      }
      {
        assertion = !useBifrost || cfg.logicalModels == [ ] || lib.all
          (model: builtins.elem model cfg.logicalModels) mappedLogicalModels;
        message = "Bifrost mappings may contain only configured logicalModels.";
      }
      {
        assertion = !useBifrost || cfg.logicalModels == [ ] || lib.all
          (model: builtins.elem model mappedLogicalModels) cfg.logicalModels;
        message = "Every Bifrost logical model requires at least one native mapping.";
      }
      {
        assertion = !useBifrost || lib.all (model: builtins.elem model mappedLogicalModels) ruleModels;
        message = "Bifrost routing rules must reference mapped logical models.";
      }
      {
        assertion = !useBifrost || lib.all (id: builtins.elem id providerIds) ruleProviderIds;
        message = "Bifrost routing rules must reference enabled provider IDs.";
      }
      {
        assertion = !useBifrost || lib.all
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
        MemoryDenyWriteExecute = true;
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
