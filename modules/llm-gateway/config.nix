{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.llm-gateway;
  activeUpstreams = lib.filterAttrs (_: upstream: upstream.enable) cfg.upstreams;

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
    credential = {
      type = "api_keys";
      api_keys = [ ];
    };
    overrides.header = upstream.headerOverrides;
  };

  publicConfig = {
    inherit (cfg) host port;
    local_api_key = null;
    log_level = cfg.logLevel;
    model_list_prefix = cfg.modelListPrefix;
    model_list_prefix_default_on_migrated = true;
    retryable_failure_cooldown_secs = cfg.retryableFailureCooldownSeconds;
    same_upstream_retry_count = cfg.sameUpstreamRetryCount;
    upstream_strategy = {
      order = cfg.routing.order;
      inherit dispatch;
    };
    hot_model_mappings = { };
    upstreams = lib.mapAttrsToList publicUpstream activeUpstreams;
  };

  publicConfigFile = pkgs.writeText "llm-gateway-public-config.json" (builtins.toJSON publicConfig);
  dataDir = "/run/${cfg.runtimeDirectory}";
  runtimeConfigFile = "${dataDir}/config.jsonc";

  credentialName = upstreamName: index:
    "upstream-${lib.replaceStrings [ "." "_" ] [ "-" "-" ] upstreamName}-${toString index}";

  upstreamCredentials = lib.concatLists (lib.mapAttrsToList
    (name: upstream: lib.imap0
      (index: file: {
        inherit file;
        name = credentialName name index;
        upstreamId = upstream.id;
      })
      upstream.apiKeyFiles)
    activeUpstreams);

  loadCredentials =
    lib.optional (cfg.clientCredentialFile != null) "client-key:${cfg.clientCredentialFile}"
    ++ map (credential: "${credential.name}:${credential.file}") upstreamCredentials;

  upstreamIds = map (upstream: upstream.id) (builtins.attrValues activeUpstreams);
  credentialNames = map (credential: credential.name) upstreamCredentials;
  advertisedModels = lib.unique (lib.concatMap
    (upstream: upstream.availableModels)
    (builtins.attrValues activeUpstreams));
  mappingCovers = upstream: model:
    builtins.hasAttr model upstream.modelMappings || builtins.hasAttr "*" upstream.modelMappings;

  appendCredential = credential: ''
    next="$tmp.next"
    ${pkgs.jq}/bin/jq \
      --arg id ${lib.escapeShellArg credential.upstreamId} \
      --rawfile raw "$CREDENTIALS_DIRECTORY/${credential.name}" '
        ($raw | sub("[\\r\\n]+$"; "")) as $secret
        | if ($secret == "" or ($secret | test("[\\r\\n]")))
          then error("invalid upstream credential") else . end
        | .upstreams |= map(
            if .id == $id then .credential.api_keys += [$secret] else . end
          )
      ' "$tmp" > "$next"
    mv "$next" "$tmp"
  '';

  runtimeConfigBuilder = ''
    set -eu
    umask 077

    install -d -m 0700 ${lib.escapeShellArg dataDir}
    tmp=$(mktemp ${lib.escapeShellArg "${runtimeConfigFile}.XXXXXX"})
    trap 'rm -f "$tmp" "$tmp.next"' EXIT

    ${if cfg.clientCredentialFile != null then (
      ''
        ${pkgs.jq}/bin/jq --rawfile raw "$CREDENTIALS_DIRECTORY/client-key" '
          ($raw | sub("[\\r\\n]+$"; "")) as $secret
          | if ($secret == "" or ($secret | test("[\\r\\n]")))
            then error("invalid client credential") else . end
          | .local_api_key = $secret
        ' ${publicConfigFile} > "$tmp"
      ''
    ) else (
      ''
        # No client credential configured: gateway stays open (local_api_key = null).
        cp ${publicConfigFile} "$tmp"
      ''
    )}

    ${lib.concatMapStringsSep "\n" appendCredential upstreamCredentials}

    chmod 0600 "$tmp"
    mv "$tmp" ${lib.escapeShellArg runtimeConfigFile}
  '';
in
{
  config = lib.mkIf cfg.enable {
    lattice.llm-gateway.publicConfigFile = publicConfigFile;

    assertions = [
      {
        assertion = activeUpstreams != { };
        message = "lattice.llm-gateway requires at least one enabled upstream.";
      }
      {
        assertion = lib.all (upstream: upstream.providers != [ ]) (builtins.attrValues activeUpstreams);
        message = "Every enabled LLM gateway upstream must declare at least one provider.";
      }
      {
        assertion = lib.all (upstream: upstream.apiKeyFiles != [ ]) (builtins.attrValues activeUpstreams);
        message = "Every enabled LLM gateway upstream must provide at least one apiKeyFile.";
      }
      {
        assertion = builtins.length upstreamIds == builtins.length (lib.unique upstreamIds);
        message = "Enabled LLM gateway upstream IDs must be unique.";
      }
      {
        assertion = builtins.length credentialNames == builtins.length (lib.unique credentialNames);
        message = "LLM gateway upstream names produce duplicate systemd credential names.";
      }
      {
        assertion = cfg.routing.dispatch == "serial" || cfg.routing.maxParallel >= 2;
        message = "race and hedged LLM gateway dispatch require maxParallel >= 2.";
      }
      {
        assertion = builtins.length cfg.logicalModels == builtins.length (lib.unique cfg.logicalModels);
        message = "lattice.llm-gateway.logicalModels must not contain duplicates.";
      }
      {
        assertion = cfg.logicalModels == [ ] || !cfg.modelListPrefix;
        message = "A logical model contract requires modelListPrefix = false.";
      }
      {
        assertion = cfg.logicalModels == [ ] || lib.all
          (model: builtins.elem model cfg.logicalModels)
          advertisedModels;
        message = "LLM gateway upstreams may advertise only configured logicalModels.";
      }
      {
        assertion = cfg.logicalModels == [ ] || lib.all
          (model: builtins.elem model advertisedModels)
          cfg.logicalModels;
        message = "Every logical model must be advertised by at least one LLM gateway upstream.";
      }
      {
        assertion = cfg.logicalModels == [ ] || lib.all
          (upstream: lib.all (mappingCovers upstream) upstream.availableModels)
          (builtins.attrValues activeUpstreams);
        message = "Every advertised logical model must map to a provider-specific model ID.";
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
