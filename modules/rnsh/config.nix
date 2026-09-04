{ config, lib, ... }:

let
  cfg = config.lattice.rnsh;
  repeated = flag: values: lib.concatMap (value: [ flag value ]) values;
  flags =
    [ "-l" "--config" cfg.homeDir "--rnsconfig" cfg.configDir ]
    ++ lib.optionals (cfg.identity != null) [ "--identity" cfg.identity ]
    ++ lib.optionals (cfg.service != null) [ "--service" cfg.service ]
    ++ lib.optionals (cfg.announcePeriod != null) [ "--announce" (toString cfg.announcePeriod) ]
    ++ repeated "--allowed" cfg.allowed
    ++ lib.optionals cfg.noAuth [ "--no-auth" ]
    ++ lib.optionals cfg.remoteCommandAsArgs [ "--remote-command-as-args" ]
    ++ lib.optionals cfg.noRemoteCommand [ "--no-remote-command" ]
    ++ lib.optionals cfg.noId [ "--no-id" ]
    ++ lib.optionals cfg.mirrorExit [ "--mirror" ]
    ++ lib.optionals cfg.base256 [ "--base256" ]
    ++ lib.optionals (cfg.timeout != null) [ "--timeout" (toString cfg.timeout) ]
    ++ lib.optionals (cfg.verbose > 0) [ "-${lib.concatStrings (lib.replicate cfg.verbose "v")}" ]
    ++ lib.optionals (cfg.quiet > 0) [ "-${lib.concatStrings (lib.replicate cfg.quiet "q")}" ]
    ++ cfg.extraArgs
    ++ lib.optionals (cfg.command != [ ]) ([ "--" ] ++ cfg.command);
in
{
  config = lib.mkIf cfg.enable {
    users.groups.${cfg.group} = { };
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = lib.mkDefault cfg.homeDir;
    };

    systemd.services.rnsh = {
      description = "Reticulum remote shell listener";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" "rns-server.service" ];
      wants = [ "network-online.target" ];
      path = [ cfg.package ];
      environment.HOME = cfg.homeDir;
      preStart = ''
        install -d -m 0750 -o ${lib.escapeShellArg cfg.user} -g ${lib.escapeShellArg cfg.group} ${lib.escapeShellArg cfg.homeDir}
      '';
      serviceConfig = {
        User = cfg.user;
        Group = cfg.group;
        ExecStart = "${lib.getExe cfg.package} ${lib.escapeShellArgs flags}";
        Restart = "on-failure";
        RestartSec = 5;
        UMask = "0077";
        WorkingDirectory = cfg.homeDir;
      };
    };
  };
}
