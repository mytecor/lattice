{ config, lib, pkgs, ... }:

let
  cfg = config.lattice.rnsh;
in
{
  config = lib.mkMerge [
    {
      lattice.rnsh = {
        enable = lib.mkDefault true;
        configDir = lib.mkDefault config.lattice.rns-server.configDir;
        user = lib.mkDefault "rnsh";
        group = lib.mkDefault "rnsh";
        identity = lib.mkDefault "${cfg.homeDir}/identity";
        announcePeriod = lib.mkDefault 3600;
      };
    }
    (lib.mkIf cfg.enable {
      users.users.${cfg.user}.extraGroups = [ config.lattice.rns-server.group ];
      systemd.services.rnsh.requires = [ "rns-server.service" ];
      # rns-server starts a child daemon; After= alone does not wait for its sockets.
      # The RPC listener starts after the shared socket, on both Linux and macOS.
      systemd.services.rnsh.preStart = lib.mkAfter ''
        ready=false
        for attempt in $(seq 1 100); do
          if ${pkgs.netcat-openbsd}/bin/nc -z 127.0.0.1 ${toString config.lattice.rns-server.reticulum.instance_control_port}; then
            ready=true
            break
          fi
          sleep 0.1
        done
        if [ "$ready" != true ]; then
          echo "Reticulum shared daemon did not become ready" >&2
          exit 1
        fi
      '';
      assertions = [
        {
          assertion = config.lattice.rns-server.enable;
          message = "lattice.rnsh: the profile requires lattice.rns-server.enable.";
        }
        {
          assertion = config.lattice.rns-server.reticulum.share_instance;
          message = "lattice.rnsh: the profile requires a shared Reticulum instance.";
        }
        {
          assertion = !cfg.noAuth && cfg.allowed != [ ];
          message = "lattice.rnsh: the profile requires an explicit allowed identity and noAuth = false.";
        }
      ];
    })
  ];
}
