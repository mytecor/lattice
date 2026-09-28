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
    # All credentials are passed as environment variables: the config only
    # names the env var, the gateway resolves it at startup from its process
    # environment (env.<name>) and never from a file. A provider without a
    # configured key emits null and is treated as keyless.
    api_key = if provider.apiKeySecretFile != null then "env.${provider.apiKeyEnv}" else null;
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

  # Flat routing table is the single source of truth: no generated pipelines.
  # Every rule (entry, subroute, transition target) is written by hand in
  # cfg.routingRules and validated by the shared rewriteRule evaluator.
  allRules = cfg.routingRules;

  dataDir = "/run/${cfg.runtimeDirectory}";

  publicConfig = {
    inherit (cfg) host port;
    metrics_host = cfg.metricsHost;
    metrics_port = cfg.metricsPort;
    log_level = cfg.logLevel;
    # Named client keys; the runtime config carries only the non-secret key id
    # and the env-var reference, never the key material.
    client_api_keys = map (client: { id = client.id; api_key = "env.${client.env}"; }) cfg.clientKeys;
    catalog_refresh_interval = cfg.catalogRefreshInterval;
    stream_idle_timeout = cfg.streamIdleTimeout;
    affinity_file = if (cfg.affinityFile != null) then cfg.affinityFile else "${dataDir}/affinity.json";
    providers = lib.mapAttrsToList publicProvider activeProviders;
    routing_rules = map publicRule allRules;
  };

  publicConfigFile = pkgs.writeText "llm-gateway-public-config.json" (builtins.toJSON publicConfig);
  runtimeConfigFile = "${dataDir}/config.json";

  # Every credential — per-provider API keys and per-client keys alike — reaches
  # the gateway process as an environment variable, never as a file read by the
  # gateway itself. agenix stays the at-rest store: the module loads each secret
  # into a dedicated oneshot unit with systemd LoadCredential, writes one
  # `NAME='value'` line per credential (NAME = the config-declared env var) into
  # a 0600 EnvironmentFile, chowns it to the unprivileged gateway user, and the
  # gateway service reads it via EnvironmentFile at process spawn. The runtime
  # config and the EnvironmentFile reference the same env names by construction,
  # so they can never drift.
  envCredentials =
    lib.mapAttrsToList
      (name: provider: { env = provider.apiKeyEnv; file = provider.apiKeySecretFile; })
      (lib.filterAttrs (_: provider: provider.apiKeySecretFile != null) activeProviders)
    ++ map (client: { env = client.env; file = client.secretFile; }) cfg.clientKeys;

  envDir = "/run/llm-gateway-env";
  envFile = "${envDir}/keys.env";

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
      # The fallback/retry/hedge target pools come from their own subroute
      # filter+map rules only: the discriminated transition actions own no
      # providers field, so this is guaranteed structurally at Nix evaluation
      # time.
      {
        assertion = lib.all
          (provider: provider.apiKeySecretFile == null || provider.apiKeyEnv != null)
          (builtins.attrValues activeProviders);
        message = "llm-gateway: apiKeySecretFile requires a non-null apiKeyEnv.";
      }
      {
        assertion = builtins.length cfg.clientKeys == builtins.length (lib.unique (map (client: client.id) cfg.clientKeys));
        message = "llm-gateway: client key ids must be unique.";
      }
    ];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
    };

    # Materializes the API keys as an environment file. Runs as root so it can
    # receive the LoadCredential secret files; the resulting EnvironmentFile is
    # owned by the unprivileged gateway user. Restarting/rotating the gateway
    # (the documented `restartUnits = [ "llm-gateway.service" ]`) re-runs this
    # unit first via PartOf, so the env file is always rebuilt before the
    # gateway spawns.
    systemd.services.llm-gateway-env = {
      description = "Materialize llm-gateway API keys as environment variables";
      before = [ "llm-gateway.service" ];
      requiredBy = [ "llm-gateway.service" ];
      partOf = [ "llm-gateway.service" ];
      serviceConfig = {
        Type = "oneshot";
        RuntimeDirectory = "llm-gateway-env";
        RuntimeDirectoryMode = "0700";
        UMask = "0077";
        # Credential name == the config-declared env var name, so the env file
        # lines and the runtime config reference the same names by construction.
        LoadCredential = map (credential: "${credential.env}:${credential.file}") envCredentials;
        ExecStart = pkgs.writeShellScript "llm-gateway-env" ''
          set -eu
          umask 077
          dir=${lib.escapeShellArg envDir}
          out="$dir/keys.env"
          : > "$out"
          creds=${lib.escapeShellArgs (map (credential: credential.env) envCredentials)}
          if [ -n "$creds" ]; then
            for cred in $creds; do
              value=$(tr -d '\r\n' < "$CREDENTIALS_DIRECTORY/$cred")
              printf "%s='%s'\n" "$cred" "$value" >> "$out"
            done
          fi
          chmod 0600 "$out"
          chown ${cfg.user}:${cfg.group} "$dir" "$out"
        '';
      };
    };

    systemd.services.llm-gateway = {
      description = "Lattice OpenAI-compatible LLM gateway";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" "llm-gateway-env.service" ];
      wants = [ "network-online.target" ];
      preStart = ''
        set -eu
        umask 077
        tmp=$(mktemp ${lib.escapeShellArg "${runtimeConfigFile}.XXXXXX"})
        trap 'rm -f "$tmp"' EXIT
        cp ${lib.escapeShellArg publicConfigFile} "$tmp"
        chmod 0600 "$tmp"
        mv "$tmp" ${lib.escapeShellArg runtimeConfigFile}
      '';
      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        RuntimeDirectory = cfg.runtimeDirectory;
        RuntimeDirectoryMode = "0700";
        RuntimeDirectoryPreserve = "restart";
        WorkingDirectory = dataDir;
        EnvironmentFile = envFile;
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
