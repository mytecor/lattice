{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.ipfs-registry-facade;
in
{
  config = lib.mkIf cfg.enable {
    # 1. Enable local Kubo IPFS daemon with pinned persistent storage
    services.kubo = {
      enable = true;
      package = cfg.kuboPackage;
      dataDir = cfg.dataDir;
      settings = {
        Addresses = {
          API = [ cfg.ipfsApiAddress ];
          # The OCI facade talks to the RPC API directly. A public HTTP
          # gateway is unnecessary and may collide with another local service.
          Gateway = [ ];
        };
      };
    };

    # services.kubo does not set StateDirectory for a non-default dataDir.
    # Let systemd establish ownership before Kubo's ExecStartPre touches the
    # repository; this also works when the path is an impermanence bind mount.
    systemd.services.ipfs.serviceConfig = {
      StateDirectory = lib.mkForce "ipfs-daemon";
      StateDirectoryMode = lib.mkForce "0750";
    };

    # 2. Add nerdctl and kubo to system packages for CLI operations
    environment.systemPackages = [ cfg.nerdctlPackage cfg.kuboPackage ];

    # 3. nerdctl ipfs registry serve systemd service (OCI facade over IPFS)
    systemd.services.ipfs-registry-facade = {
      description = "Containerd OCI registry facade over IPFS";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" "ipfs.service" ];
      requires = [ "ipfs.service" ];

      serviceConfig = {
        ExecStart = "${lib.getExe cfg.nerdctlPackage} ipfs registry serve --listen-registry ${cfg.listenAddress}:${toString cfg.port} --ipfs-address ${cfg.ipfsApiAddress}";
        Restart = "on-failure";
        RestartSec = 3;

        # Sandboxing
        NoNewPrivileges = true;
        ProtectSystem = "full";
        ProtectHome = true;
        PrivateTmp = true;
        ProtectControlGroups = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
      };
    };
  };
}
