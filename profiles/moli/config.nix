{ config, lib, pkgs, ... }:
let
  ports = import ../networking/ports.nix;
  sandbox = {
    Restart = "on-failure";
    RestartSec = 3;
    DynamicUser = true;
    NoNewPrivileges = true;
    PrivateTmp = true;
    PrivateDevices = true;
    ProtectSystem = "strict";
    ProtectHome = true;
    ProtectKernelTunables = true;
    ProtectKernelModules = true;
    ProtectControlGroups = true;
    RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
    CapabilityBoundingSet = "";
  };
in {
  config = lib.mkMerge [
    { lattice.moli.enable = lib.mkDefault true; }
    (lib.mkIf config.lattice.moli.enable {
      assertions = [{
        assertion = config.lattice.authentik.enable
          && lib.any (entry: entry.service == "browser") config.lattice.authentik.forwardAuth;
        message = "Moli remote debugging requires Authentik ForwardAuth for the browser service.";
      }];
      fonts.fontconfig.enable = true;
      systemd.services.moli = {
        description = "Moli remote debugging browser";
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" ];
        serviceConfig = sandbox // {
          ExecStart = "${lib.getExe pkgs.lattice.moli} serve --host 127.0.0.1 --port ${toString ports.moli-cdp} --layout --resource";
          # V8 needs executable memory; do not set MemoryDenyWriteExecute.
        };
      };
      systemd.services.moli-ingress = {
        description = "Moli CDP discovery and WebSocket proxy";
        wantedBy = [ "multi-user.target" ];
        after = [ "moli.service" ];
        wants = [ "moli.service" ];
        serviceConfig = sandbox // {
          ExecStart = "${lib.getExe pkgs.lattice.moli-ingress} --listen 127.0.0.1:${toString ports.moli-ingress} --upstream http://127.0.0.1:${toString ports.moli-cdp} --frontend ${pkgs.lattice.devtools-frontend}";
          MemoryDenyWriteExecute = true;
        };
      };
    })
  ];
}
