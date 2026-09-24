{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.llm-gateway;
  activeProviders = lib.filterAttrs (_: provider: provider.enable) cfg.providers;

  shared = import ./types.nix { inherit lib; };
  inherit (shared) rewriteRule;

  publicProvider = _name: provider: {
    inherit (provider) id priority headers;
    base_provider = provider.baseProvider;
    inference_url = provider.inferenceUrl;
    models_url = provider.modelsUrl;
    api_key = null;
    models_api_key = null;
    cooldown = provider.cooldown;
    request_timeout = provider.requestTimeout;
    bifrost_max_retries = provider.bifrostMaxRetries;
    allow_private_network = provider.allowPrivateNetwork;
    strip_params = provider.stripParams;
    set_params = provider.setParams;
  };

  # Emits exactly the fields a routing action owns plus the rule envelope
  # (route + action). The action-specific fields come from the discriminated
  # submodule's internal `_public` projection, so the generated public JSON
  # stays clean (no spurious defaulted fields from other actions) and mirrors
  # the gateway's per-action strict decoder. A filter carries its single
  # `where` dimension; a transition (retry/fallback/hedge) points at a named
  # subroute through `target`. The logical model registry is derived from the
  # entry route filters (raw and generated), so there is no separate
  # native-model config field.
  publicRule = rule:
    {
      route = rule.route;
      action = rule.action;
    }
    // rule._public;

  # Pipeline coalescing for one logical model: per-model override →
  # deployment-level default → built-in default. `path` addresses the shared
  # optional pipeline shape; unset fields are null on both levels.
  pipelineValue = model: path: default:
    let
      override = lib.attrByPath path null model.pipeline;
      deployment = lib.attrByPath path null cfg.pipeline;
    in
    if override != null then override
    else if deployment != null then deployment
    else default;

  # modelPlan generates the canonical bounded pipeline for one logical model
  # through the same typed rule evaluator the raw routingRules use, so the
  # emitted JSON is identical to a handwritten config and receives the full
  # per-action validation during Nix evaluation. Per route:
  # filter model → filter provider → map → rank → balance → affinity → race →
  # retry → [hedge] → semaphore → timeout, plus the `<model>.retry` (and, when
  # hedge is enabled, `<model>.hedge`) named subroutes that re-select unused
  # providers.
  modelPlan = name: model:
    let
      pick = path: default: pipelineValue model path default;
      # Enabled provider IDs (not attrnames): the provider registry allows a
      # custom `id` per instance.
      providerList = let p = pick [ "providers" ] null; in if p != null then p else map (provider: provider.id) (builtins.attrValues activeProviders);
      # Per-provider native overrides: a provider listed in nativeByProvider is
      # mapped to its own native ID instead of model.native. The pool is split
      # into one group per distinct effective native, emitting filter provider
      # (in group) → map native pairs, so different providers of one logical
      # model can be reached through different native IDs in a single stage
      # (f7-10: one provider may appear in the pool once; each group maps
      # disjoint providers to one native).
      nativeFor = provider: model.nativeByProvider.${provider} or model.native;
      # Debounced distinct natives in providerList order: each provider's
      # effective native is emitted once, in first-appearance order.
      distinctNatives = lib.foldl' (acc: provider:
        let n = nativeFor provider; in
        if lib.elem n acc then acc else acc ++ [ n ]
      ) [ ] providerList;
      # effectiveNativeGroups: list of { native, providers } — one group per
      # distinct effective native, providers in providerList order. Independent
      # providers map to their own native; without nativeByProvider this is a
      # single group over the whole provider list (identical to previous
      # output).
      effectiveNativeGroups = map (n: {
        native = n;
        providers = lib.filter (p: nativeFor p == n) providerList;
      }) distinctNatives;
      # filter+map rules for one route (entry or subroute) whose provider
      # selection carries an optional unused restriction.
      mapSliceRules = route: unused:
        lib.concatMap (g: [
          { route = route; action = "filter"; where = { provider = { "in" = g.providers; } // lib.optionalAttrs unused { unused = true; }; }; }
          { route = route; action = "map"; native = g.native; }
        ]) effectiveNativeGroups;
      entry = name;
      retryRoute = "${entry}.retry";
      hedgeEnabled = pick [ "hedge" "enable" ] false;
      hedgeRoute = "${entry}.hedge";
      rawRules = [
        { route = entry; action = "filter"; where = { model = { eq = entry; }; }; }
      ]
      # One filter provider (in group) → map native pair per distinct
      # effective native; without nativeByProvider this is a single pair over
      # the whole provider list, identical to previous output.
      ++ mapSliceRules entry false
      ++ [
        { route = entry; action = "rank"; strategy = "priority"; }
        {
          route = entry;
          action = "balance";
          strategy = pick [ "balance" "strategy" ] "p2c";
          weights = pick [ "balance" "weights" ] { };
          window = pick [ "balance" "window" ] "5m";
          errorBudget = pick [ "balance" "errorBudget" ] 0.2;
        }
        {
          route = entry;
          action = "affinity";
          sources = [ "responses.conversation" "responses.previous_response_id" ];
          ttl = pick [ "affinityTtl" ] "24h";
          onMissing = "ignore";
          onProviderFailure = "fail-closed";
        }
        { route = entry; action = "race"; count = pick [ "raceCount" ] 1; }
        {
          route = entry;
          action = "retry";
          target = retryRoute;
          attempts = pick [ "retry" "attempts" ] 2;
          backoffType = pick [ "retry" "backoffType" ] "exponential";
          backoffInitial = pick [ "retry" "backoffInitial" ] "200ms";
          backoffMax = pick [ "retry" "backoffMax" ] "1s";
        }
      ]
      # Hedge is opt-in (f7-13 live run: a latency hedge re-concentrates
      # completions on the fastest provider while the primary choice stays
      # distributed).
      ++ lib.optionals (pick [ "hedge" "enable" ] false) [
        { route = entry; action = "hedge"; after = pick [ "hedge" "after" ] "3s"; target = hedgeRoute; }
      ]
      ++ [
        {
          route = entry;
          action = "semaphore";
          maxCalls = pick [ "semaphore" "maxCalls" ] 4;
          maxInFlight = pick [ "semaphore" "maxInFlight" ] 3;
          maxCallsPerProvider = pick [ "semaphore" "maxCallsPerProvider" ] 1;
        }
        { route = entry; action = "timeout"; duration = pick [ "timeout" "duration" ] "60s"; }
      ]
      # Continue (in-gateway stream takeover) is declared last on the entry
      # route. Opt-in per pipeline; when enabled every logical model gets it.
      ++ lib.optionals (pick [ "continue" "enable" ] false) [
        {
          route = entry;
          action = "continue";
          idle = pick [ "continue" "idle" ] "90s";
          reshare = pick [ "continue" "reshare" ] "full";
          retries = pick [ "continue" "retries" ] 0;
          wait = pick [ "continue" "wait" ] null;
        }
      ]
      # Repetition (in-gateway loop guard) is an opt-in companion to continue,
      # declared after it on the entry route. Off by default: an absent action
      # arms nothing and the relay behaves exactly as before.
      ++ lib.optionals (pick [ "repetition" "enable" ] false) [
        {
          route = entry;
          action = "repetition";
          repeats = pick [ "repetition" "repeats" ] null;
          minLen = pick [ "repetition" "minLen" ] null;
          maxLen = pick [ "repetition" "maxLen" ] null;
        }
      ]
      ++ [
        # Retry subroute: applies only to the listed failures and re-selects
        # unused providers, one target per retry entry.
        #
        # The retryable set is the gateway's full retryable class universe
        # (allRetryableClasses), including live upstream 404 ("404") and the
        # catalog pre-dispatch rejection ("model_not_found"). Both mean "this
        # carrier does not serve the mapped native right now" — e.g. an
        # upstream whose serving pool dropped a model while its /models list
        # still advertises it (gonka-proxy dropped DeepSeek-V4-Flash-0731 for
        # ~15 min on 2026-09-20, session 01a0bd52). Retrying re-races unused
        # providers with the same per-native mapping, and the pair-level
        # cooldown gates the failing (provider, native) so the retry lands on
        # a different carrier. Excluding these two classes made a live 404 a
        # terminal error surfaced verbatim to the client ({message: Not Found,
        # type: 404}) instead of failing over.
        {
          route = retryRoute;
          action = "filter";
          where = {
            error = { "in" = [ "404" "model_not_found" "429" "5xx" "timeout" "connection_error" "invalid_response" ]; };
          };
        }
      ]
      # Retry subroute re-selects unused providers with the same per-native
      # grouping as the entry route.
      ++ mapSliceRules retryRoute true
      ++ [
        { route = retryRoute; action = "rank"; strategy = "priority"; }
        { route = retryRoute; action = "race"; count = 1; }
      ]
      ++ lib.optionals hedgeEnabled (mapSliceRules hedgeRoute true)
      ++ lib.optionals hedgeEnabled [
        { route = hedgeRoute; action = "rank"; strategy = "priority"; }
        { route = hedgeRoute; action = "race"; count = 1; }
      ];
    in
    {
      routes = [ entry retryRoute ] ++ lib.optional hedgeEnabled hedgeRoute;
      rules = map rewriteRule rawRules;
    };

  modelPlans = lib.mapAttrsToList modelPlan cfg.models;
  generatedRules = lib.concatMap (plan: plan.rules) modelPlans;
  modelNames = builtins.attrNames cfg.models;
  # Raw rules may extend a generated entry route (typically a fallback action
  # after the generated pipeline), but re-filtering an entry model would
  # declare a second entry route for the same logical model. Raw rules may
  # still extend a generated entry route with later actions (typically a
  # `fallback` transition); only the entry filter is reserved.
  rawEntryModels = lib.unique (lib.concatMap
    (rule: if rule.action == "filter" && builtins.hasAttr "model" rule.where
      then [ rule.where.model.eq ] else [ ])
    cfg.routingRules);
  rawEntryModelsAllowed = lib.all
    (model: ! builtins.elem model modelNames)
    rawEntryModels;
  # Raw rules must not target generated subroute names: those routes are fully
  # owned by the sugar (entry + retry + optional hedge).
  generatedSubrouteNames = lib.unique (lib.concatMap
    (plan: lib.drop 1 plan.routes)
    modelPlans);
  rawRoutes = map (rule: rule.route) cfg.routingRules;

  allRules = generatedRules ++ cfg.routingRules;

  dataDir = "/run/${cfg.runtimeDirectory}";

  publicConfig = {
    inherit (cfg) host port;
    metrics_host = cfg.metricsHost;
    metrics_port = cfg.metricsPort;
    log_level = cfg.logLevel;
    client_api_key = null;
    catalog_refresh_interval = cfg.catalogRefreshInterval;
    stream_idle_timeout = cfg.streamIdleTimeout;
    affinity_file = if (cfg.affinityFile != null) then cfg.affinityFile else "${dataDir}/affinity.json";
    providers = lib.mapAttrsToList publicProvider activeProviders;
    routing_rules = map publicRule allRules;
  };

  publicConfigFile = pkgs.writeText "llm-gateway-public-config.json" (builtins.toJSON publicConfig);
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
  # Only filter provider actions carry a provider list; the other discriminated
  # actions own none, so the registry lookup is guarded by action. Both the
  # in (selection) and not_in (exclusion) lists reference provider IDs.
  filterProviderIDs = rule:
    if rule.action == "filter" && builtins.hasAttr "provider" rule.where
    then (rule.where.provider."in" or [ ]) ++ (rule.where.provider.not_in or [ ])
    else [ ];
  mappedProviderIds = lib.unique (lib.concatMap filterProviderIDs allRules);
  # balance weights are a second place where routing rules reference provider
  # IDs (static per-provider weights), checked here so a typo fails fast at
  # Nix evaluation instead of only at gateway startup.
  balanceWeightedProviders = lib.unique (lib.concatMap
    (rule: if rule.action == "balance" then builtins.attrNames rule.weights or [ ] else [ ])
    allRules);
in
{
  config = lib.mkIf cfg.enable {
    lattice.llm-gateway.publicConfigFile = publicConfigFile;

    assertions = [
      {
        assertion = activeProviders != { };
        message = "llm-gateway: Bifrost runtime requires at least one enabled provider.";
      }
      {
        assertion = builtins.length providerIds == builtins.length (lib.unique providerIds);
        message = "llm-gateway: Bifrost provider IDs must be unique.";
      }
      {
        assertion = lib.all
          (id: builtins.elem id providerIds)
          mappedProviderIds;
        message = "llm-gateway: every routing filter must reference an enabled provider ID.";
      }
      {
        assertion = lib.all
          (id: builtins.elem id providerIds)
          balanceWeightedProviders;
        message = "llm-gateway: every balance weights entry must reference an enabled provider ID.";
      }
      # nativeByProvider keys must be enabled provider IDs available to the
      # model: the sugar groups the effective provider list (not the whole
      # registry) and a key outside it would be silently dropped.
      {
        assertion = lib.all
          (name: lib.all (provider: builtins.elem provider providerIds)
            (builtins.attrNames cfg.models.${name}.nativeByProvider))
          modelNames;
        message = "llm-gateway: every nativeByProvider key must reference an enabled provider ID.";
      }
      {
        assertion = lib.all
          (name: lib.all (provider: builtins.elem provider (let m = cfg.models.${name}; p = pipelineValue m [ "providers" ] null; in if p != null then p else providerIds))
            (builtins.attrNames cfg.models.${name}.nativeByProvider))
          modelNames;
        message = "llm-gateway: every nativeByProvider key must be in the model's effective provider list (models.<name>.nativeByProvider targets a provider excluded by models.<name>.pipeline.providers or the deployment pipeline).";
      }
      {
        assertion = lib.all
          (name: name != "" && builtins.match "[^.]*" name != null)
          modelNames;
        message = "llm-gateway: a model name must be non-empty and dot-free: dots are the subroute naming convention (model.retry / model.hedge).";
      }
      {
        assertion = rawEntryModelsAllowed;
        message = "llm-gateway: routingRules must not re-filter a model declared in models (duplicate entry route for the same logical model).";
      }
      {
        assertion = lib.all (route: ! builtins.elem route generatedSubrouteNames) rawRoutes;
        message = "llm-gateway: routingRules must not declare rules on generated subroute names (<model>.retry / <model>.hedge): those routes are owned by the models sugar.";
      }
      # The fallback/retry/hedge target pools come from their own subroute
      # filter+map rules only: the discriminated transition actions own no
      # providers field, so this is guaranteed structurally at Nix evaluation
      # time.
      {
        assertion = lib.all
          (provider: provider.modelsApiKeyFile == null || provider.modelsUrl != null)
          (builtins.attrValues activeProviders);
        message = "llm-gateway: modelsApiKeyFile requires modelsUrl.";
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
        RuntimeDirectoryPreserve = "restart";
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
