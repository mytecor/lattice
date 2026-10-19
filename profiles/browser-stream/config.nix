{ config, lib, pkgs, ... }:
let
  ports = import ../networking/ports.nix;
in {
  config = lib.mkMerge [
    { lattice.browser-stream.enable = lib.mkDefault true; }
    (lib.mkIf config.lattice.browser-stream.enable {
      # Reuse the agent's browser. The endpoint is never accepted from query input.
      assertions = [
        {
          assertion = config.lattice.foxbridge-camoufox.enable;
          message = "browser-stream requires the Foxbridge/Camoufox browser runtime.";
        }
        {
          assertion = config.lattice.authentik.enable
            && lib.any (entry: entry.service == "browser") config.lattice.authentik.forwardAuth;
          message = "browser-stream requires Authentik ForwardAuth for the browser service.";
        }
      ];
      systemd.services.browser-stream = {
        description = "PhantomStream browser viewer over Foxbridge CDP";
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" "foxbridge-camoufox.service" ];
        wants = [ "foxbridge-camoufox.service" ];
        environment = {
          BROWSER_CDP_URL = "http://127.0.0.1:${toString config.lattice.foxbridge-camoufox.port}";
          BROWSER_STREAM_PORT = toString ports.browser-stream;
        };
        serviceConfig = {
          ExecStart = lib.getExe pkgs.lattice.browser-stream;
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
      };
    })
  ];
}
