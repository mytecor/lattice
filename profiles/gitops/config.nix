{ lib, pkgs, ... }:

let
  branch = "main";
  latticeRepository = (import ../radicle/repositories.nix).lattice;
  sourceStateDir = "/var/lib/comin/source";
in
{
  config = {
    services.comin = {
      enable = true;
      remotes = [
        {
          name = "source";
          url = "${sourceStateDir}/repository";
          branches.${branch}.name = branch;
          branches.testing.name = "";
        }
      ];
    };

    systemd.services = {
      comin = {
        after = [ "lattice-comin-source-sync.service" ];
        wants = [ "lattice-comin-source-sync.service" ];
      };

      lattice-comin-source-sync = {
        description = "Normalize Lattice GitOps source history for comin";
        before = [ "comin.service" ];
        wantedBy = [ "multi-user.target" ];
        environment = {
          LATTICE_GITOPS_STATE_DIR = sourceStateDir;
          LATTICE_GITOPS_RADICLE_REMOTE = latticeRepository.storagePath;
          LATTICE_GITOPS_ORIGIN_REMOTE = "https://github.com/mytecor/lattice.git";
          LATTICE_GITOPS_BRANCH = branch;
          LATTICE_GITOPS_COMIN_STATE_DIR = "/var/lib/comin";
        };
        serviceConfig = {
          Type = "oneshot";
          ExecStart = lib.getExe pkgs.lattice.comin-source-sync;
          TimeoutStartSec = "6min";
          UMask = "0077";
        };
      };
    };

    systemd.timers.lattice-comin-source-sync = {
      description = "Periodically refresh the normalized Lattice GitOps source";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "1min";
        OnUnitActiveSec = "1min";
        Unit = "lattice-comin-source-sync.service";
      };
    };
  };
}
