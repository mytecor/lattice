{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.llm-gateway;
  dataDir = "/run/${cfg.runtimeDirectory}";
  runtimeConfigFile = "${dataDir}/config.json";

  rawConfigFile = pkgs.writeText "llm-gateway-config.json" (builtins.toJSON cfg.settings);
  publicConfigFile = pkgs.runCommand "llm-gateway-checked-config.json" {
    nativeBuildInputs = [ cfg.package ];
  } ''
    llm-gateway \
      --require-env-secrets \
      --allowed-env ${lib.escapeShellArg (lib.concatStringsSep "," credentialNames)} \
      --config ${rawConfigFile} \
      check
    cp ${rawConfigFile} "$out"
  '';

  envDir = "/run/llm-gateway-env";
  envFile = "${envDir}/keys.env";
  credentialNames = builtins.attrNames cfg.credentials;
  envCredentials = map (name: {
    inherit name;
    file = cfg.credentials.${name};
  }) credentialNames;
in
{
  config = lib.mkIf cfg.enable {
    lattice.llm-gateway.publicConfigFile = publicConfigFile;

    assertions = [
      {
        assertion = lib.all
          (name: builtins.match "[A-Za-z_][A-Za-z0-9_]*" name != null)
          credentialNames;
        message = "llm-gateway: credential names must be valid environment variable names.";
      }
    ];

    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/empty";
    };

    systemd.services.llm-gateway-env = {
      description = "Materialize llm-gateway credentials as environment variables";
      before = [ "llm-gateway.service" ];
      requiredBy = [ "llm-gateway.service" ];
      partOf = [ "llm-gateway.service" ];
      serviceConfig = {
        Type = "oneshot";
        RuntimeDirectory = "llm-gateway-env";
        RuntimeDirectoryMode = "0700";
        RuntimeDirectoryPreserve = "yes";
        UMask = "0077";
        LoadCredential = map (credential: "${credential.name}:${credential.file}") envCredentials;
        ExecStart = pkgs.writeShellScript "llm-gateway-env" ''
          set -eu
          umask 077
          dir=${lib.escapeShellArg envDir}
          out="$dir/keys.env"
          : > "$out"
          creds="${lib.concatStringsSep " " credentialNames}"
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
